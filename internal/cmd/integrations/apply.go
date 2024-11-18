// Copyright 2021 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package integrations

import (
	"encoding/json"
	"errors"
	"fmt"
	"internal/apiclient"
	"internal/client/authconfigs"
	"internal/client/connections"
	"internal/client/integrations"
	"internal/client/sfdc"
	"internal/clilog"
	"internal/cmd/utils"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

// ApplyCmd a scaffold Integrations
var ApplyCmd = &cobra.Command{
	Use:   "apply",
	Short: "Apply configuration generated by scaffold to a region",
	Long:  "Apply configuration generated by scaffold to a region",
	Args: func(cmd *cobra.Command, args []string) (err error) {
		cmdProject := cmd.Flag("proj")
		cmdRegion := cmd.Flag("reg")

		if err = apiclient.SetRegion(cmdRegion.Value.String()); err != nil {
			return err
		}
		if folder == "" && (pipeline == "" || release == "" || outputGCSPath == "") {
			return fmt.Errorf("atleast one of folder or pipeline, release and outputGCSPath must be supplied")
		}
		if folder != "" && (pipeline != "" || release != "" || outputGCSPath != "") {
			return fmt.Errorf("both folder and pipeline, release and outputGCSPath cannot be supplied")
		}
		if (pipeline != "" && (release == "" || outputGCSPath == "")) ||
			(release != "" && (pipeline == "" && outputGCSPath == "")) ||
			(outputGCSPath != "" && (pipeline == "" && release == "")) {
			return fmt.Errorf("release, pipeline and outputGCSPath must be set")
		}
		return apiclient.SetProjectID(cmdProject.Value.String())
	},
	RunE: func(cmd *cobra.Command, args []string) (err error) {
		var skaffoldConfigUri string

		if folder == "" {
			skaffoldConfigUri, err = apiclient.GetCloudDeployGCSLocations(pipeline, release)
			if err != nil {
				return err
			}
			folder, err = apiclient.ExtractTgz(skaffoldConfigUri)
			if err != nil {
				return err
			}
		}

		srcFolder := folder
		if env != "" {
			folder = path.Join(folder, env)
		}
		if stat, err := os.Stat(folder); err != nil || !stat.IsDir() {
			return fmt.Errorf("problem with supplied path, %w", err)
		}

		createSecret, _ := strconv.ParseBool(cmd.Flag("create-secret").Value.String())
		grantPermission, _ := strconv.ParseBool(cmd.Flag("grant-permission").Value.String())
		wait, _ := strconv.ParseBool(cmd.Flag("wait").Value.String())

		integrationFolder := path.Join(srcFolder, "src")
		authconfigFolder := path.Join(folder, "authconfigs")
		connectorsFolder := path.Join(folder, "connectors")
		customConnectorsFolder := path.Join(folder, "custom-connectors")
		configVarsFolder := path.Join(folder, "config-variables")
		overridesFile := path.Join(folder, "overrides/overrides.json")
		sfdcinstancesFolder := path.Join(folder, "sfdcinstances")
		sfdcchannelsFolder := path.Join(folder, "sfdcchannels")
		endpointsFolder := path.Join(folder, "endpoints")
		zonesFolder := path.Join(folder, "zones")

		apiclient.DisableCmdPrintHttpResponse()

		if !skipAuthconfigs {
			if err = processAuthConfigs(authconfigFolder); err != nil {
				return err
			}
		} else {
			clilog.Info.Printf("Skipping applying authconfigs configuration\n")
		}

		if err = processEndpoints(endpointsFolder); err != nil {
			return err
		}

		if err = processManagedZones(zonesFolder); err != nil {
			return err
		}

		if !skipConnectors {
			if err = processCustomConnectors(customConnectorsFolder); err != nil {
				return err
			}

			if err = processConnectors(connectorsFolder, grantPermission, createSecret, wait); err != nil {
				return err
			}
		} else {
			clilog.Info.Printf("Skipping applying connector configuration\n")
		}

		if err = processSfdcInstances(sfdcinstancesFolder); err != nil {
			return err
		}

		if err = processSfdcChannels(sfdcchannelsFolder); err != nil {
			return err
		}

		if err = processIntegration(overridesFile, integrationFolder,
			configVarsFolder, pipeline, grantPermission); err != nil {
			return err
		}

		return err
	},
}

var serviceAccountName, serviceAccountProject, encryptionKey, pipeline, release, outputGCSPath string

func init() {
	grantPermission, createSecret, wait := false, false, false

	ApplyCmd.Flags().StringVarP(&folder, "folder", "f",
		"", "Folder containing scaffolding configuration")
	ApplyCmd.Flags().StringVarP(&pipeline, "pipeline", "",
		"", "Cloud Deploy Pipeline name")
	ApplyCmd.Flags().StringVarP(&release, "release", "",
		"", "Cloud Deploy Release name")
	ApplyCmd.Flags().StringVarP(&outputGCSPath, "output-gcs-path", "",
		"", "Upload a file named results.json containing the results")
	ApplyCmd.Flags().BoolVarP(&grantPermission, "grant-permission", "g",
		false, "Grant the service account permission to the GCP resource; default is false")
	ApplyCmd.Flags().StringVarP(&userLabel, "userlabel", "u",
		"", "Integration version userlabel")
	ApplyCmd.Flags().StringVarP(&serviceAccountName, "sa", "",
		"", "Service Account name for the connection or integration trigger")
	ApplyCmd.Flags().StringVarP(&serviceAccountProject, "sp", "",
		"", "Service Account Project for the connection or integraton trigger.")
	ApplyCmd.Flags().StringVarP(&encryptionKey, "encryption-keyid", "k",
		"", "Cloud KMS key for decrypting Auth Config; Format = locations/*/keyRings/*/cryptoKeys/*")
	ApplyCmd.Flags().StringVarP(&env, "env", "e",
		"", "Environment name for the scaffolding")
	ApplyCmd.Flags().BoolVarP(&createSecret, "create-secret", "",
		false, "Create Secret Manager secrets when creating the connection; default is false")
	ApplyCmd.Flags().BoolVarP(&wait, "wait", "",
		false, "Waits for the connector to finish, with success or error; default is false")
	ApplyCmd.Flags().BoolVarP(&skipConnectors, "skip-connectors", "",
		false, "Skip applying connector configuration; default is false")
	ApplyCmd.Flags().BoolVarP(&skipAuthconfigs, "skip-authconfigs", "",
		false, "Skip applying authconfigs configuration; default is false")
	ApplyCmd.Flags().BoolVarP(&useUnderscore, "use-underscore", "",
		false, "Use underscore as a file splitter; default is __")
}

func getFilenameWithoutExtension(filname string) string {
	return strings.TrimSuffix(filname, filepath.Ext(filname))
}

func getVersion(respBody []byte) (version string, err error) {
	var jsonMap map[string]interface{}

	if err = json.Unmarshal(respBody, &jsonMap); err != nil {
		return "", err
	}

	if jsonMap["name"] == "" {
		return "", errors.New("version not found")
	}

	if version = filepath.Base(fmt.Sprintf("%s", jsonMap["name"])); version == "" {
		return "", errors.New("version not found")
	}
	return version, nil
}

func getServiceAttachment(respBody []byte) (sa string, err error) {
	var jsonMap map[string]string

	if err = json.Unmarshal(respBody, &jsonMap); err != nil {
		return "", err
	}
	if jsonMap["serviceAttachment"] == "" {
		return "", errors.New("serviceAttachment not found")
	}
	return jsonMap["serviceAttachment"], nil
}

func processAuthConfigs(authconfigFolder string) (err error) {
	var stat fs.FileInfo
	rJSONFiles := regexp.MustCompile(`(\S*)\.json`)

	if stat, err = os.Stat(authconfigFolder); err == nil && stat.IsDir() {
		// create any authconfigs
		err = filepath.Walk(authconfigFolder, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if !info.IsDir() {
				authConfigFile := filepath.Base(path)
				if rJSONFiles.MatchString(authConfigFile) {
					clilog.Info.Printf("Found configuration for authconfig: %s\n", authConfigFile)
					version, _ := authconfigs.Find(getFilenameWithoutExtension(authConfigFile), "")
					// create the authconfig only if the version was not found
					if version == "" {
						authConfigBytes, err := utils.ReadFile(path)
						if err != nil {
							return err
						}
						clilog.Info.Printf("Creating authconfig: %s\n", authConfigFile)
						if _, err = authconfigs.Create(authConfigBytes); err != nil {
							return err
						}
					} else {
						clilog.Info.Printf("Authconfig %s already exists\n", authConfigFile)
					}
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func processEndpoints(endpointsFolder string) (err error) {
	var stat fs.FileInfo
	rJSONFiles := regexp.MustCompile(`(\S*)\.json`)

	if stat, err = os.Stat(endpointsFolder); err == nil && stat.IsDir() {
		// create any endpoint attachments
		err = filepath.Walk(endpointsFolder, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if !info.IsDir() {
				endpointFile := filepath.Base(path)
				if rJSONFiles.MatchString(endpointFile) {
					clilog.Info.Printf("Found configuration for endpoint attachment: %s\n", endpointFile)
				}
				if !connections.FindEndpoint(getFilenameWithoutExtension(endpointFile)) {
					// the endpoint does not exist, try to create it
					endpointBytes, err := utils.ReadFile(path)
					if err != nil {
						return err
					}
					serviceAccountName, err := getServiceAttachment(endpointBytes)
					if err != nil {
						return err
					}
					if _, err = connections.CreateEndpoint(getFilenameWithoutExtension(endpointFile),
						serviceAccountName, "", false); err != nil {
						return err
					}
				} else {
					clilog.Info.Printf("Endpoint %s already exists\n", endpointFile)
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func processManagedZones(zonesFolder string) (err error) {
	var stat fs.FileInfo
	rJSONFiles := regexp.MustCompile(`(\S*)\.json`)

	// create any managed zones
	if stat, err = os.Stat(zonesFolder); err == nil && stat.IsDir() {
		// create any managedzones
		err = filepath.Walk(zonesFolder, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if !info.IsDir() {
				zoneFile := filepath.Base(path)
				if rJSONFiles.MatchString(zoneFile) {
					clilog.Info.Printf("Found configuration for managed zone: %s\n", zoneFile)
				}
				if _, err = connections.GetZone(getFilenameWithoutExtension(zoneFile), true); err != nil {
					// the managed zone does not exist, try to create it
					zoneBytes, err := utils.ReadFile(path)
					if err != nil {
						return err
					}
					if _, err = connections.CreateZone(getFilenameWithoutExtension(zoneFile),
						zoneBytes); err != nil {
						return err
					}
				} else {
					clilog.Info.Printf("Zone %s already exists\n", zoneFile)
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func processConnectors(connectorsFolder string, grantPermission bool, createSecret bool, wait bool) (err error) {
	var stat fs.FileInfo
	rJSONFiles := regexp.MustCompile(`(\S*)\.json`)

	if stat, err = os.Stat(connectorsFolder); err == nil && stat.IsDir() {
		// create any connectors
		err = filepath.Walk(connectorsFolder, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if !info.IsDir() {
				connectionFile := filepath.Base(path)
				if rJSONFiles.MatchString(connectionFile) {
					clilog.Info.Printf("Found configuration for connection: %s\n", connectionFile)
					_, err = connections.Get(getFilenameWithoutExtension(connectionFile), "", true, false)
					// create the connection only if the connection is not found
					if err != nil {
						connectionBytes, err := utils.ReadFile(path)
						if err != nil {
							return err
						}
						clilog.Info.Printf("Creating connector: %s\n", connectionFile)

						if _, err = connections.Create(getFilenameWithoutExtension(connectionFile),
							connectionBytes,
							serviceAccountName,
							serviceAccountProject,
							encryptionKey,
							grantPermission,
							createSecret,
							wait); err != nil {
							return err
						}
					} else {
						clilog.Info.Printf("Connector %s already exists\n", connectionFile)
					}
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func processCustomConnectors(customConnectorsFolder string) (err error) {
	var stat fs.FileInfo
	var fileSplitter string
	rJSONFiles := regexp.MustCompile(`(\S*)\.json`)

	if useUnderscore {
		fileSplitter = utils.LegacyFileSplitter
	} else {
		fileSplitter = utils.DefaultFileSplitter
	}

	if stat, err = os.Stat(customConnectorsFolder); err == nil && stat.IsDir() {
		// create any custom connectors
		err = filepath.Walk(customConnectorsFolder, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if !info.IsDir() {
				customConnectionFile := filepath.Base(path)
				if rJSONFiles.MatchString(customConnectionFile) {
					customConnectionDetails := strings.Split(strings.TrimSuffix(customConnectionFile, filepath.Ext(customConnectionFile)), fileSplitter)
					// the file format is name-version.json
					if len(customConnectionDetails) == 2 {
						clilog.Info.Printf("Found configuration for custom connection: %v\n", customConnectionFile)
						contents, err := utils.ReadFile(path)
						if err != nil {
							return err
						}
						clilog.Info.Printf("Creating custom connector: %s\n", customConnectionFile)
						if _, err := connections.GetCustomVersion(customConnectionDetails[0],
							customConnectionDetails[1], false); err != nil {
							// didn't find the custom connector, create it
							if err = connections.CreateCustomWithVersion(customConnectionDetails[0],
								customConnectionDetails[1], contents, serviceAccountName, serviceAccountProject); err != nil {
								return err
							}
						} else {
							clilog.Info.Printf("Custom Connector %s already exists\n", customConnectionFile)
						}
					}
				}
			}
			return nil
		})
	}
	return nil
}

func processSfdcInstances(sfdcinstancesFolder string) (err error) {
	var stat fs.FileInfo
	rJSONFiles := regexp.MustCompile(`(\S*)\.json`)

	if stat, err = os.Stat(sfdcinstancesFolder); err == nil && stat.IsDir() {
		// create any sfdc instances
		err = filepath.Walk(sfdcinstancesFolder, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if !info.IsDir() {
				instanceFile := filepath.Base(path)
				if rJSONFiles.MatchString(instanceFile) {
					clilog.Info.Printf("Found configuration for sfdc instance: %s\n", instanceFile)
					_, err = sfdc.GetInstance(getFilenameWithoutExtension(instanceFile), true)
					// create the instance only if the sfdc instance is not found
					if err != nil {
						instanceBytes, err := utils.ReadFile(path)
						if err != nil {
							return err
						}
						clilog.Info.Printf("Creating sfdc instance: %s\n", instanceFile)
						_, err = sfdc.CreateInstanceFromContent(instanceBytes)
						if err != nil {
							return nil
						}
					} else {
						clilog.Info.Printf("sfdc instance %s already exists\n", instanceFile)
					}
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func processSfdcChannels(sfdcchannelsFolder string) (err error) {
	var stat fs.FileInfo
	var fileSplitter string
	rJSONFiles := regexp.MustCompile(`(\S*)\.json`)
	const sfdcNamingConvention = 2 // when file is split with _, the result must be 2

	if useUnderscore {
		fileSplitter = utils.LegacyFileSplitter
	} else {
		fileSplitter = utils.DefaultFileSplitter
	}

	if stat, err = os.Stat(sfdcchannelsFolder); err == nil && stat.IsDir() {
		// create any sfdc channels
		err = filepath.Walk(sfdcchannelsFolder, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if !info.IsDir() {
				channelFile := filepath.Base(path)
				if rJSONFiles.MatchString(channelFile) {
					clilog.Info.Printf("Found configuration for sfdc channel: %s\n", channelFile)
					sfdcNames := strings.Split(getFilenameWithoutExtension(channelFile), fileSplitter)
					if len(sfdcNames) != sfdcNamingConvention {
						clilog.Warning.Printf("sfdc chanel file %s does not follow the naming "+
							"convention instanceName_channelName.json\n", channelFile)
						return nil
					}
					version, _, err := sfdc.FindChannel(sfdcNames[1], sfdcNames[0])
					// create the instance only if the sfdc channel is not found
					if err != nil {
						channelBytes, err := utils.ReadFile(path)
						if err != nil {
							return err
						}
						clilog.Info.Printf("Creating sfdc channel: %s\n", channelFile)
						_, err = sfdc.CreateChannelFromContent(version, channelBytes)
						if err != nil {
							return nil
						}
					} else {
						clilog.Info.Printf("sfdc channel %s already exists\n", channelFile)
					}
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func processIntegration(overridesFile string, integrationFolder string,
	configVarsFolder string, pipeline string, grantPermission bool,
) (err error) {
	rJSONFiles := regexp.MustCompile(`(\S*)\.json`)

	var integrationNames []string
	var overridesBytes []byte

	javascriptFolder := path.Join(integrationFolder, "javascript")
	jsonnetFolder := path.Join(integrationFolder, "datatransformer")

	if _, err = os.Stat(overridesFile); err == nil {
		overridesBytes, err = utils.ReadFile(overridesFile)
		if err != nil {
			return err
		}
	}

	if len(overridesBytes) > 0 {
		clilog.Info.Printf("Found overrides file %s\n", overridesFile)
	}

	// get the integration file
	_ = filepath.Walk(integrationFolder, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			integrationFile := filepath.Base(path)
			if rJSONFiles.MatchString(integrationFile) {
				clilog.Info.Printf("Found configuration for integration: %s\n", integrationFile)
				integrationNames = append(integrationNames, integrationFile)
			}
		}
		return nil
	})

	if len(integrationNames) > 0 {
		// get only the first file
		integrationBytes, err := utils.ReadFile(path.Join(integrationFolder, integrationNames[0]))
		if err != nil {
			return err
		}
		// check for code files
		codeMap, err := processCodeFolders(javascriptFolder, jsonnetFolder)
		if err != nil {
			return err
		}

		if len(codeMap) > 0 {
			integrationBytes, err = integrations.SetCode(integrationBytes, codeMap)
			if err != nil {
				return err
			}
		}

		clilog.Info.Printf("Create integration %s\n", getFilenameWithoutExtension(integrationNames[0]))
		respBody, err := integrations.CreateVersion(getFilenameWithoutExtension(integrationNames[0]),
			integrationBytes, overridesBytes, "", userLabel, grantPermission)
		if err != nil {
			return err
		}
		version, err := getVersion(respBody)
		if err != nil {
			return err
		}

		// create  test cases for integration
		if err = processTestCases(integrationFolder, getFilenameWithoutExtension(integrationNames[0]), version); err != nil {
			return err
		}

		// publish the integration
		clilog.Info.Printf("Publish integration %s with version %s\n",
			getFilenameWithoutExtension(integrationNames[0]), version)
		// read any config variables
		configVarsFile := path.Join(configVarsFolder, getFilenameWithoutExtension(integrationNames[0])+"-config.json")
		var configVarBytes []byte
		if _, err = os.Stat(configVarsFile); err == nil {
			configVarBytes, err = utils.ReadFile(configVarsFile)
			if err != nil {
				return err
			}
		}
		_, err = integrations.Publish(getFilenameWithoutExtension(integrationNames[0]), version, configVarBytes)
		if err != nil {
			return err
		}
		if pipeline != "" {
			err = apiclient.WriteResultsFile(outputGCSPath, "SUCCEEDED")
		}
		return err
	}
	clilog.Warning.Printf("No integration files were found\n")
	return nil
}

func processCodeFolders(javascriptFolder string, jsonnetFolder string) (codeMap map[string]map[string]string, err error) {
	codeMap = make(map[string]map[string]string)
	codeMap["JavaScriptTask"] = make(map[string]string)
	codeMap["JsonnetMapperTask"] = make(map[string]string)
	rJavaScriptFiles := regexp.MustCompile(`javascript_\d{1,2}.js`)
	rJsonnetFiles := regexp.MustCompile(`datatransformer_\d{1,2}.jsonnet`)
	var javascriptNames, jsonnetNames []string

	_ = filepath.Walk(javascriptFolder, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			javascriptFile := filepath.Base(path)
			if rJavaScriptFiles.MatchString(javascriptFile) {
				clilog.Info.Printf("Found JavaScript file for integration: %s\n", javascriptFile)
				javascriptNames = append(javascriptNames, javascriptFile)
			}
		}
		return nil
	})

	if len(javascriptNames) > 0 {
		for _, javascriptName := range javascriptNames {
			javascriptBytes, err := utils.ReadFile(path.Join(javascriptFolder, javascriptName))
			if err != nil {
				return nil, err
			}
			codeMap["JavaScriptTask"][strings.ReplaceAll(getFilenameWithoutExtension(javascriptName), "javascript_", "")] =
				strings.ReplaceAll(string(javascriptBytes), "\n", "\\n")
		}
	}

	_ = filepath.Walk(jsonnetFolder, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			jsonnetFile := filepath.Base(path)
			if rJsonnetFiles.MatchString(jsonnetFile) {
				clilog.Info.Printf("Found Jsonnet file for integration: %s\n", jsonnetFile)
				jsonnetNames = append(jsonnetNames, jsonnetFile)
			}
		}
		return nil
	})

	if len(jsonnetNames) > 0 {
		for _, jsonnetName := range jsonnetNames {
			jsonnetBytes, err := utils.ReadFile(path.Join(jsonnetFolder, jsonnetName))
			if err != nil {
				return nil, err
			}
			codeMap["JsonnetMapperTask"][strings.ReplaceAll(getFilenameWithoutExtension(jsonnetName), "datatransformer_", "")] =
				strings.ReplaceAll(string(jsonnetBytes), "\n", "\\n")
		}
	}

	return codeMap, nil
}

func processTestCases(testCasesFolder string, integrationName string, version string) (err error) {
	rJSONFiles := regexp.MustCompile(`(\S*)\.json`)

	var testCaseFiles []string

	_ = filepath.Walk(testCasesFolder, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			testCaseFile := filepath.Base(path)
			if rJSONFiles.MatchString(testCaseFile) {
				clilog.Info.Printf("Found test case file %s for integration: %s\n", testCaseFile, integrationName)
				testCaseFiles = append(testCaseFiles, testCaseFile)
			}
		}
		return nil
	})

	if len(testCaseFiles) > 0 {
		for _, testCaseFile := range testCaseFiles {
			testCaseBytes, err := utils.ReadFile(path.Join(testCasesFolder, testCaseFile))
			if err != nil {
				return err
			}
			_, err = integrations.CreateTestCase(integrationName, version, string(testCaseBytes))
			if err != nil {
				return err
			}
		}
	}
	return nil
}
