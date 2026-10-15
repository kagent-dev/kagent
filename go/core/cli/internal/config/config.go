// Package config resolves local CLI settings before command execution.
package config

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/spf13/cobra"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/known/durationpb"

	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
	shared "github.com/kagent-dev/kagent/go/core/pkg/config"
	"github.com/kagent-dev/kagent/go/pkg/logging"
)

type contextKey struct{}

// FromContext returns the settings resolved by the root command.
func FromContext(ctx context.Context) *configv1.CLIConfig {
	cfg, _ := ctx.Value(contextKey{}).(*configv1.CLIConfig)
	return cfg
}

func Register(root *cobra.Command) {
	configCommand := newCommand()
	root.PersistentFlags().String("config", "", "Configuration file (default: user config directory/kagent/config.yaml)")
	root.PersistentFlags().String("log-level", "info", "Log level: debug, info, warn, error")
	root.PersistentPreRunE = func(cmd *cobra.Command, _ []string) error {
		if (cmd.Parent() == root && cmd.Name() == "version") || (cmd.Parent() == configCommand && cmd.Name() == "schema") {
			return nil
		}
		if cmd.Parent() == configCommand {
			component, _ := cmd.Flags().GetString("component")
			if component != "cli" {
				return nil
			}
		}
		return initialize(cmd)
	}
	root.AddCommand(configCommand)
}

func initialize(cmd *cobra.Command) error {
	path, _ := cmd.Flags().GetString("config")
	explicit := cmd.Flags().Changed("config")
	if !explicit {
		dir, err := os.UserConfigDir()
		if err != nil {
			return fmt.Errorf("resolve user config directory: %w", err)
		}
		path = filepath.Join(dir, "kagent", "config.yaml")
	}
	cfg := &configv1.CLIConfig{}
	if err := shared.Load(path, cfg); err != nil && (explicit || !errors.Is(err, os.ErrNotExist)) {
		return err
	}
	shared.DefaultCLI(cfg)
	shared.ResolveCLIPaths(cfg, path)
	// Resolve paths before flags: flag paths are relative to the working directory.
	strings := map[string]*string{
		"api-url": cfg.Connection.ApiUrl, "gateway-url": cfg.Connection.GatewayUrl,
		"namespace": cfg.Connection.Namespace, "user-id": cfg.Connection.UserId,
		"ca-file": &cfg.Connection.CaFile, "server-name": &cfg.Connection.ServerName,
		"output-format": cfg.Output.Format, "log-level": cfg.Logging.Level,
	}
	for name, value := range strings {
		flag := cmd.Flags().Lookup(name)
		if flag == nil {
			continue
		}
		if flag.Changed {
			*value = flag.Value.String()
		} else if err := flag.Value.Set(*value); err != nil {
			return err
		}
	}
	if cmd.Flags().Changed("verbose") {
		cfg.Connection.Verbose, _ = cmd.Flags().GetBool("verbose")
	} else if err := cmd.Flags().Lookup("verbose").Value.Set(strconv.FormatBool(cfg.GetConnection().GetVerbose())); err != nil {
		return err
	}
	if cmd.Flags().Changed("timeout") {
		timeout, err := cmd.Flags().GetDuration("timeout")
		if err != nil {
			return err
		}
		cfg.Connection.Timeout = durationpb.New(timeout)
	} else if err := cmd.Flags().Lookup("timeout").Value.Set(cfg.GetConnection().GetTimeout().AsDuration().String()); err != nil {
		return err
	}
	if err := shared.Validate(cfg); err != nil {
		return err
	}
	logger, err := logging.New(cmd.ErrOrStderr(), cfg.GetLogging().GetLevel())
	if err != nil {
		return err
	}
	ctx := logging.IntoContext(cmd.Context(), logger)
	cmd.SetContext(context.WithValue(ctx, contextKey{}, cfg))
	return nil
}

func newCommand() *cobra.Command {
	var component string
	parent := &cobra.Command{Use: "config", Short: "Inspect and validate configuration"}
	parent.PersistentFlags().StringVar(&component, "component", "cli", "Configuration type: cli or controller")
	for _, action := range []string{"validate", "view", "schema"} {
		parent.AddCommand(&cobra.Command{Use: action, Args: cobra.NoArgs, Short: map[string]string{"validate": "Validate configuration without connecting to services", "view": "Print effective configuration without expanding secret files", "schema": "Print the protobuf descriptor set, including validation rules"}[action], RunE: func(cmd *cobra.Command, _ []string) error {
			var message proto.Message
			switch component {
			case "cli":
				message = &configv1.CLIConfig{}
			case "controller":
				message = &configv1.ControllerConfig{}
			default:
				return fmt.Errorf("component must be cli or controller")
			}
			if action == "schema" {
				return writeJSON(cmd, descriptorSet(message.ProtoReflect().Descriptor().ParentFile()))
			}
			if component == "cli" {
				message = FromContext(cmd.Context())
			} else {
				path, _ := cmd.Flags().GetString("config")
				if path == "" {
					return fmt.Errorf("controller configuration requires --config")
				}
				if err := shared.Load(path, message); err != nil {
					return err
				}
				cfg := message.(*configv1.ControllerConfig)
				shared.DefaultController(cfg)
				shared.ResolveControllerPaths(cfg, path)
				if err := shared.Validate(cfg); err != nil {
					return err
				}
			}
			if action == "validate" {
				_, err := fmt.Fprintln(cmd.OutOrStdout(), "Configuration is valid")
				return err
			}
			return writeJSON(cmd, message)
		}})
	}
	return parent
}

func writeJSON(cmd *cobra.Command, message proto.Message) error {
	data, err := (protojson.MarshalOptions{Indent: "  "}).Marshal(message)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(cmd.OutOrStdout(), string(data))
	return err
}

func descriptorSet(file protoreflect.FileDescriptor) *descriptorpb.FileDescriptorSet {
	result := &descriptorpb.FileDescriptorSet{}
	seen := map[string]bool{}
	var visit func(protoreflect.FileDescriptor)
	visit = func(file protoreflect.FileDescriptor) {
		if seen[file.Path()] {
			return
		}
		seen[file.Path()] = true
		for i := 0; i < file.Imports().Len(); i++ {
			visit(file.Imports().Get(i).FileDescriptor)
		}
		result.File = append(result.File, protodesc.ToFileDescriptorProto(file))
	}
	visit(file)
	return result
}
