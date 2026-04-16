package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/apimount/apimount/internal/core/auth"
)

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Manage OAuth2 authentication",
	Long: `Authenticate with OAuth2 providers using the device-code flow.

  apimount auth login  --profile github   # interactive device-code login
  apimount auth status --profile github   # show token status
  apimount auth logout --profile github   # clear cached token`,
}

var authLoginCmd = &cobra.Command{
	Use:   "login",
	Short: "Authenticate via OAuth2 device-code flow",
	Long: `Initiates an OAuth2 device-code flow. Displays a URL and user code —
open the URL in your browser and enter the code to authorize apimount.

The token is cached to disk (~/.apimount/tokens/) and reused automatically.
Use --profile to associate the token with a named profile.

Required flags (or set in profile):
  --auth-oauth2-client-id
  --auth-oauth2-token-url
  --auth-oauth2-device-url`,
	RunE: runAuthLogin,
}

var authLogoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Clear cached OAuth2 token",
	RunE:  runAuthLogout,
}

var authStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show OAuth2 token status",
	RunE:  runAuthStatus,
}

func init() {
	authLoginCmd.Flags().String("auth-oauth2-device-url", "", "OAuth2 device authorization endpoint URL")

	authCmd.AddCommand(authLoginCmd, authLogoutCmd, authStatusCmd)
}

func tokenCacheDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".apimount", "tokens")
}

func cacheKeyFromFlags() string {
	key := v.GetString("profile")
	if key == "" {
		key = "default"
	}
	return key
}

func runAuthLogin(cmd *cobra.Command, _ []string) error {
	clientID := v.GetString("auth-oauth2-client-id")
	tokenURL := v.GetString("auth-oauth2-token-url")
	deviceURL, _ := cmd.Flags().GetString("auth-oauth2-device-url")
	scopes := v.GetStringSlice("auth-oauth2-scopes")

	if clientID == "" {
		return fmt.Errorf("--auth-oauth2-client-id is required")
	}
	if tokenURL == "" {
		return fmt.Errorf("--auth-oauth2-token-url is required")
	}
	if deviceURL == "" {
		return fmt.Errorf("--auth-oauth2-device-url is required")
	}

	cache, err := auth.NewTokenCache(tokenCacheDir())
	if err != nil {
		return err
	}

	clientSecret := v.GetString("auth-oauth2-client-secret")

	provider, err := auth.NewOAuth2DeviceProvider(auth.OAuth2DeviceConfig{
		ClientID:      clientID,
		ClientSecret:  clientSecret,
		TokenURL:      tokenURL,
		DeviceAuthURL: deviceURL,
		Scopes:        scopes,
		TokenCache:    cache,
		CacheKey:      cacheKeyFromFlags(),
	})
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	fmt.Fprintln(os.Stderr, "Starting device-code authorization flow...")
	fmt.Fprintln(os.Stderr)

	tok, err := provider.Login(ctx, func(prompt auth.DevicePrompt) {
		fmt.Fprintf(os.Stderr, "Open this URL in your browser:\n\n  %s\n\n", prompt.VerificationURI)
		fmt.Fprintf(os.Stderr, "Enter this code: %s\n\n", prompt.UserCode)
		fmt.Fprintln(os.Stderr, "Waiting for authorization...")
	})
	if err != nil {
		return fmt.Errorf("login failed: %w", err)
	}

	fmt.Fprintln(os.Stderr)
	fmt.Fprintf(os.Stderr, "Authenticated successfully. Token cached as profile %q.\n", cacheKeyFromFlags())
	if !tok.Expiry.IsZero() {
		fmt.Fprintf(os.Stderr, "Token expires: %s\n", tok.Expiry.Format(time.RFC3339))
	}
	if tok.RefreshToken != "" {
		fmt.Fprintln(os.Stderr, "Refresh token stored — token will auto-renew.")
	}
	return nil
}

func runAuthLogout(_ *cobra.Command, _ []string) error {
	cache, err := auth.NewTokenCache(tokenCacheDir())
	if err != nil {
		return err
	}

	key := cacheKeyFromFlags()
	if err := cache.Delete(key); err != nil {
		return fmt.Errorf("logout failed: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Token for profile %q removed.\n", key)
	return nil
}

func runAuthStatus(_ *cobra.Command, _ []string) error {
	cache, err := auth.NewTokenCache(tokenCacheDir())
	if err != nil {
		return err
	}

	key := cacheKeyFromFlags()
	tok := cache.Get(key)
	if tok == nil {
		_, _ = fmt.Fprintf(os.Stdout, "Profile %q: no cached token\n", key)
		return nil
	}

	_, _ = fmt.Fprintf(os.Stdout, "Profile %q:\n", key)
	_, _ = fmt.Fprintf(os.Stdout, "  Token type:    %s\n", tok.Type())
	if !tok.Expiry.IsZero() {
		remaining := time.Until(tok.Expiry).Truncate(time.Second)
		if remaining > 0 {
			_, _ = fmt.Fprintf(os.Stdout, "  Expires:       %s (%s remaining)\n", tok.Expiry.Format(time.RFC3339), remaining)
		} else {
			_, _ = fmt.Fprintf(os.Stdout, "  Expires:       %s (EXPIRED)\n", tok.Expiry.Format(time.RFC3339))
		}
	}
	if tok.RefreshToken != "" {
		_, _ = fmt.Fprintf(os.Stdout, "  Refresh token: present (auto-renew enabled)\n")
	}
	_, _ = fmt.Fprintf(os.Stdout, "  Access token:  %s...%s\n", tok.AccessToken[:4], tok.AccessToken[len(tok.AccessToken)-4:])
	return nil
}
