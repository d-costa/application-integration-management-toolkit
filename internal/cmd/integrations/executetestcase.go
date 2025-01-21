// Copyright 2024 Google LLC
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
	"errors"
	"internal/apiclient"
	"internal/client/integrations"
	"internal/clilog"
	"internal/cmd/utils"
	"os"

	"github.com/spf13/cobra"
)

// ExecuteTestCaseCmd to get integration flow
var ExecuteTestCaseCmd = &cobra.Command{
	Use:   "execute",
	Short: "Execute an integration flow version test case",
	Long:  "Execute an integration flow version test case",
	Args: func(cmd *cobra.Command, args []string) (err error) {
		cmdProject := utils.GetStringParam(cmd.Flag("proj"))
		cmdRegion := utils.GetStringParam(cmd.Flag("reg"))
		testCaseID := utils.GetStringParam(cmd.Flag("test-case-id"))
		version := utils.GetStringParam(cmd.Flag("ver"))
		userLabel := utils.GetStringParam(cmd.Flag("user-label"))
		snapshot := utils.GetStringParam(cmd.Flag("snapshot"))
		inputFile := utils.GetStringParam(cmd.Flag("input-file"))
		inputFolder := utils.GetStringParam(cmd.Flag("input-folder"))

		if err = apiclient.SetRegion(cmdRegion); err != nil {
			return err
		}
		if err = validate(version, userLabel, snapshot, false); err != nil {
			return err
		}

		if inputFile != "" && testCaseID == "" {
			return errors.New("test case id must be set with input-file")
		}

		if inputFile == "" && inputFolder == "" {
			return errors.New("at least one of input-file or input-folder must be passed")
		}

		if inputFile != "" && inputFolder != "" {
			return errors.New("only one of input-file or input-folder can be passed")
		}

		if inputFolder != "" && testCaseID != "" {
			return errors.New("test case id cannot be set with input-folder")
		}

		return apiclient.SetProjectID(cmdProject)
	},
	RunE: func(cmd *cobra.Command, args []string) (err error) {
		cmd.SilenceUsage = true

		var integrationBody []byte
		version := utils.GetStringParam(cmd.Flag("ver"))
		userLabel := utils.GetStringParam(cmd.Flag("user-label"))
		snapshot := utils.GetStringParam(cmd.Flag("snapshot"))
		name := utils.GetStringParam(cmd.Flag("name"))
		testCaseID := utils.GetStringParam(cmd.Flag("test-case-id"))
		inputFile := utils.GetStringParam(cmd.Flag("input-file"))
		inputFolder := utils.GetStringParam(cmd.Flag("input-folder"))

		apiclient.DisableCmdPrintHttpResponse()
		defer apiclient.EnableCmdPrintHttpResponse()

		if snapshot != "" {
			integrationBody, err = integrations.GetBySnapshot(name, snapshot, true, false, false)
		} else if userLabel != "" {
			integrationBody, err = integrations.GetByUserlabel(name, userLabel, true, false, false)
		}
		version, err = getIntegrationVersion(integrationBody)
		if err != nil {
			return err
		}

		if inputFile != "" {
			if _, err := os.Stat(inputFile); os.IsNotExist(err) {
				return err
			}

			content, err := os.ReadFile(inputFile)
			if err != nil {
				return err
			}

			_, err = integrations.ExecuteTestCase(name, version, testCaseID, string(content))
			if err == nil {
				clilog.Info.Printf("Test case %s executed successfully\n", testCaseID)
			}
		}
		if inputFolder != "" {
			return executeAllTestCases(inputFolder, name, version)
		}
		return err
	},
}

func init() {
	var name, version, testCaseID, inputFile, inputFolder, userLabel, snapshot string

	ExecuteTestCaseCmd.Flags().StringVarP(&name, "name", "n",
		"", "Integration flow name")
	ExecuteTestCaseCmd.Flags().StringVarP(&version, "ver", "v",
		"", "Integration flow version")
	ExecuteTestCaseCmd.Flags().StringVarP(&userLabel, "user-label", "u",
		"", "Integration flow user label")
	ExecuteTestCaseCmd.Flags().StringVarP(&snapshot, "snapshot", "s",
		"", "Integration flow snapshot number")
	ExecuteTestCaseCmd.Flags().StringVarP(&testCaseID, "test-case-id", "c",
		"", "Test Case ID")
	ExecuteTestCaseCmd.Flags().StringVarP(&inputFile, "input-file", "f",
		"", "Path to a file containing input parameters. For a sample see ./samples/test-config.json")
	ExecuteTestCaseCmd.Flags().StringVarP(&inputFolder, "input-folder", "d",
		"", "Path to a folder containing files for test case execution. File names MUST match display names")

	_ = ExecuteTestCaseCmd.MarkFlagRequired("name")

}
