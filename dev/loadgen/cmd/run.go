// Copyright (c) 2020 Gitpod GmbH. All rights reserved.
// Licensed under the GNU Affero General Public License (AGPL).
// See License-AGPL.txt in the project root for license information.

package cmd

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"io/ioutil"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/protobuf/types/known/timestamppb"

	csapi "github.com/gitpod-io/gitpod/content-service/api"
	"github.com/gitpod-io/gitpod/loadgen/pkg/loadgen"
	"github.com/gitpod-io/gitpod/loadgen/pkg/observer"
	"github.com/gitpod-io/gitpod/ws-manager/api"
)

var runOpts struct {
	TLSPath string
}

// runCmd represents the run command
var runCmd = &cobra.Command{
	Use:   "run",
	Short: "runs the load generator",
	Run: func(cmd *cobra.Command, args []string) {
		const workspaceCount = 1

		var load loadgen.LoadGenerator
		load = loadgen.NewFixedLoadGenerator(500*time.Millisecond, 300*time.Millisecond)
		load = loadgen.NewWorkspaceCountLimitingGenerator(load, workspaceCount)

		template := &api.StartWorkspaceRequest{
			Id: "will-be-overriden",
			Metadata: &api.WorkspaceMetadata{
				MetaId:    "will-be-overriden",
				Owner:     "00000000-0000-0000-0000-000000000000",
				StartedAt: timestamppb.Now(),
			},
			ServicePrefix: "will-be-overriden",
			Spec: &api.StartWorkspaceSpec{
				IdeImage:         "eu.gcr.io/gitpod-dev/ide/code:commit-8c1466008dedabe79d82cbb91931a16f7ce7994c",
				Admission:        api.AdmissionLevel_ADMIT_OWNER_ONLY,
				CheckoutLocation: "gitpod",
				Git: &api.GitSpec{
					Email:    "test@gitpod.io",
					Username: "foobar",
				},
				FeatureFlags: []api.WorkspaceFeatureFlag{},
				Initializer: &csapi.WorkspaceInitializer{
					Spec: &csapi.WorkspaceInitializer_Git{
						Git: &csapi.GitInitializer{
							CheckoutLocation: "",
							CloneTaget:       "main",
							RemoteUri:        "https://github.com/gitpod-io/gitpod.git",
							TargetMode:       csapi.CloneTargetMode_REMOTE_BRANCH,
							Config: &csapi.GitConfig{
								Authentication: csapi.GitAuthMethod_NO_AUTH,
							},
						},
					},
				},
				Timeout: "5m",
				// WorkspaceImage:    "eu.gcr.io/gitpod-dev/workspace-images:3fcaad7ba5a5a4695782cb4c366b82f927f1e6c1cf0c88fd4f14d985f7eb21f6",
				WorkspaceImage:    "reg.gitpod.io:31001/remote/1e54c2a8-d419-4788-b5cc-5530a47ba584",
				WorkspaceLocation: "gitpod",
				Envvars: []*api.EnvironmentVariable{
					{
						Name:  "THEIA_SUPERVISOR_TOKENS",
						Value: `[{"tokenOTS":"https://gitpod.io/api/ots/get/12e48a26-6365-47ff-8123-396e174d3cd6","token":"ots","kind":"gitpod","host":"gitpod.io","scope":["function:getWorkspace","function:getLoggedInUser","function:getPortAuthenticationToken","function:getWorkspaceOwner","function:getWorkspaceUsers","function:isWorkspaceOwner","function:controlAdmission","function:setWorkspaceTimeout","function:getWorkspaceTimeout","function:sendHeartBeat","function:getOpenPorts","function:openPort","function:closePort","function:getLayout","function:generateNewGitpodToken","function:takeSnapshot","function:storeLayout","function:stopWorkspace","function:getToken","function:getContentBlobUploadUrl","function:getContentBlobDownloadUrl","function:accessCodeSyncStorage","function:guessGitTokenScopes","function:getEnvVars","function:setEnvVar","function:deleteEnvVar","resource:workspace::red-koi-5v89i5l0::get/update","resource:workspaceInstance::1e54c2a8-d419-4788-b5cc-5530a47ba584::get/update/delete","resource:snapshot::ws-red-koi-5v89i5l0::create","resource:gitpodToken::*::create","resource:userStorage::*::create/get/update","resource:token::*::get","resource:contentBlob::*::create/get","resource:envVar::gitpod-com/gitpod::create/get/update/delete"],"expiryDate":"2022-07-15T05:47:17.875Z","reuse":2}]`,
					},
				},
			},
			Type: api.WorkspaceType_REGULAR,
		}

		var opts []grpc.DialOption
		if runOpts.TLSPath != "" {
			ca, err := ioutil.ReadFile(filepath.Join(runOpts.TLSPath, "ca.crt"))
			if err != nil {
				log.Fatal(err)
			}
			capool := x509.NewCertPool()
			capool.AppendCertsFromPEM(ca)
			cert, err := tls.LoadX509KeyPair(filepath.Join(runOpts.TLSPath, "tls.crt"), filepath.Join(runOpts.TLSPath, "tls.key"))
			if err != nil {
				log.Fatal(err)
			}
			creds := credentials.NewTLS(&tls.Config{
				Certificates: []tls.Certificate{cert},
				RootCAs:      capool,
				ServerName:   "ws-manager",
			})
			opts = append(opts, grpc.WithTransportCredentials(creds))
		} else {
			opts = append(opts, grpc.WithInsecure())
		}

		conn, err := grpc.Dial("localhost:8080", opts...)
		if err != nil {
			log.Fatal(err)
		}
		defer conn.Close()
		client := api.NewWorkspaceManagerClient(conn)
		executor := &loadgen.WsmanExecutor{C: client}

		session := &loadgen.Session{
			Executor: executor,
			Load:     load,
			Specs:    &loadgen.FixedWorkspaceGenerator{Template: template},
			Worker:   5,
			Observer: []chan<- *loadgen.SessionEvent{
				observer.NewLogObserver(true),
				observer.NewProgressBarObserver(workspaceCount),
				observer.NewStatsObserver(func(s *observer.Stats) {
					fc, err := json.Marshal(s)
					if err != nil {
						return
					}
					os.WriteFile("stats.json", fc, 0644)
				}),
			},
			PostLoadWait: func() {
				<-make(chan struct{})
				log.Info("load generation complete - press Ctrl+C to finish of")

			},
		}

		go func() {
			sigc := make(chan os.Signal, 1)
			signal.Notify(sigc, syscall.SIGINT)
			<-sigc
			os.Exit(0)
		}()

		err = session.Run()
		if err != nil {
			log.WithError(err).Fatal()
		}

	},
}

func init() {
	rootCmd.AddCommand(runCmd)

	runCmd.Flags().StringVar(&runOpts.TLSPath, "tls", "", "path to ws-manager's TLS certificates")
}
