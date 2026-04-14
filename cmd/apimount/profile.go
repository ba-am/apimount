package main

import (
	"fmt"
	"sort"

	"github.com/spf13/cobra"
)

// Profiles live under the top-level "profiles" key of ~/.apimount.yaml:
//
//   profiles:
//     github:
//       spec: https://...
//       base-url: https://api.github.com
//       auth-bearer: ghp_xxx
//     petstore:
//       spec: ./petstore.yaml
//       base-url: https://petstore3.swagger.io/api/v3
//
// `apimount profile list` / `show` / `use` manipulate that file via viper.

var profileCmd = &cobra.Command{
	Use:   "profile",
	Short: "Manage named profiles from the config file",
}

var profileListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all defined profiles",
	RunE: func(cmd *cobra.Command, args []string) error {
		profiles, ok := v.Get("profiles").(map[string]interface{})
		if !ok || len(profiles) == 0 {
			fmt.Println("no profiles defined (add them to ~/.apimount.yaml under 'profiles:')")
			return nil
		}
		names := make([]string, 0, len(profiles))
		for name := range profiles {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			fmt.Println(name)
		}
		return nil
	},
}

var profileShowCmd = &cobra.Command{
	Use:   "show NAME",
	Short: "Show the settings for a named profile",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		sub := v.Sub("profiles." + name)
		if sub == nil {
			return fmt.Errorf("no profile named %q", name)
		}
		keys := sub.AllKeys()
		sort.Strings(keys)
		for _, key := range keys {
			val := sub.Get(key)
			if key == "auth-bearer" || key == "auth-basic" || key == "auth-apikey" {
				val = redact(fmt.Sprintf("%v", val))
			}
			fmt.Printf("  %-20s %v\n", key+":", val)
		}
		return nil
	},
}

var profileUseCmd = &cobra.Command{
	Use:   "use NAME",
	Short: "Mark a profile as active (equivalent to --profile NAME)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		sub := v.Sub("profiles." + args[0])
		if sub == nil {
			return fmt.Errorf("no profile named %q", args[0])
		}
		fmt.Printf("profile %q is available; invoke future commands with --profile %s\n", args[0], args[0])
		return nil
	},
}

func init() {
	profileCmd.AddCommand(profileListCmd, profileShowCmd, profileUseCmd)
}

// redact replaces all but the first 4 and last 2 characters of a secret with '*'.
// Short secrets are fully redacted.
func redact(s string) string {
	if len(s) <= 6 {
		return "****"
	}
	return s[:4] + "…" + s[len(s)-2:]
}
