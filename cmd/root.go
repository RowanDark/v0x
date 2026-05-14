package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"time"

	"golang.org/x/sync/errgroup"
	"golang.org/x/term"

	"github.com/RowanDark/v0x/internal/config"
	"github.com/RowanDark/v0x/internal/crawler"
	"github.com/RowanDark/v0x/internal/extractor"
	"github.com/RowanDark/v0x/internal/output"
	"github.com/spf13/cobra"
)

const banner = `
┌─────────────────────────────────┐
│  ██╗   ██╗ ██████╗ ██╗  ██╗     │
│  ██║   ██║██╔═████╗╚██╗██╔╝     │
│  ██║   ██║██║██╔██║ ╚███╔╝      │
│  ╚██╗ ██╔╝████╔╝██║ ██╔██╗      │
│   ╚████╔╝ ╚██████╔╝██╔╝ ██╗     │
│    ╚═══╝   ╚═════╝ ╚═╝  ╚═╝     │
│  wordlist generator  v1.0.0     │
│  github.com/RowanDark/v0x       │
└─────────────────────────────────┘
`

var cfg config.Config
var noHeadless bool

var rootCmd = &cobra.Command{
	Use:          "v0x --url <target> [flags]",
	Short:        "v0x — modern web wordlist generator",
	SilenceUsage: true,
	Long: `v0x crawls web pages and extracts words to build targeted wordlists.
Supports headless browser rendering via playwright-go, structured output
formats, and optional authentication.

Required:
  --url        Target URL to crawl (e.g. https://target.com)

Output defaults to stdout in txt format (CeWL-compatible).
Use --output to write to a file and --format to change output type.`,
	Example: `  v0x --url https://target.com
  v0x --url https://target.com --depth 3 --format json --output wordlist.json
  v0x --url https://target.com --auth-cookie "session=abc" --format md --output report.md`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if term.IsTerminal(int(os.Stdout.Fd())) {
			fmt.Fprint(os.Stderr, banner)
		}

		if cfg.URL == "" {
			return fmt.Errorf("--url is required")
		}
		if noHeadless {
			cfg.Headless = false
		}

		if cfg.AuthVerifySelector != "" && cfg.AuthFormURL == "" {
			fmt.Fprintln(os.Stderr, "v0x: warning: --auth-verify-selector has no effect without --auth-form-url")
		}

		authStrategyName := "none"
		switch {
		case cfg.AuthFormURL != "":
			authStrategyName = "form"
		case cfg.AuthBasicUser != "":
			authStrategyName = "basic"
		case cfg.AuthCookie != "":
			authStrategyName = "cookie"
		case cfg.AuthBearer != "" || cfg.AuthHeader != "":
			authStrategyName = "bearer"
		}

		if cfg.Verbose {
			fmt.Fprintf(os.Stderr, "v0x: crawling %s (depth=%d, headless=%v, auth=%s)\n",
				cfg.URL, cfg.Depth, cfg.Headless, authStrategyName)
		}

		formatter, err := output.New(cfg.Format)
		if err != nil {
			return err
		}

		// Open output writer before crawling — fail fast if we can't write.
		w := os.Stdout
		if cfg.Output != "" {
			f, err := os.Create(cfg.Output)
			if err != nil {
				return fmt.Errorf("opening output file: %w", err)
			}
			defer f.Close()
			w = f
		} else if term.IsTerminal(int(os.Stdout.Fd())) {
			fmt.Fprintln(os.Stderr, "--- wordlist output ---")
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		if cfg.Timeout > 0 {
			var timeoutCancel context.CancelFunc
			ctx, timeoutCancel = context.WithTimeout(ctx, cfg.Timeout)
			defer timeoutCancel()
		}

		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, os.Interrupt)
		go func() {
			select {
			case <-sigCh:
				cancel()
			case <-ctx.Done():
			}
		}()

		c := crawler.New(cfg)
		pages, err := c.Crawl(ctx, cfg)
		if err != nil {
			return fmt.Errorf("starting crawl: %w", err)
		}

		agg := extractor.NewAggregator()
		var pagesCrawled int

		g, _ := errgroup.WithContext(ctx)
		g.Go(func() error {
			for page := range pages {
				r := extractor.Extract(page.HTML, cfg)
				agg.Add(r)
				pagesCrawled++
				if cfg.Verbose {
					fmt.Fprintf(os.Stderr, "v0x: page %s — %d words\n", page.URL, len(r.Words))
				}
			}
			return nil
		})

		if err := g.Wait(); err != nil {
			return fmt.Errorf("pipeline: %w", err)
		}

		if ctx.Err() == context.DeadlineExceeded {
			fmt.Fprintf(os.Stderr, "v0x: timeout reached after %s — writing partial results\n", cfg.Timeout)
		}

		finalResult := agg.Finalize()
		meta := output.OutputMeta{
			TargetURL:    cfg.URL,
			PagesCrawled: pagesCrawled,
		}

		if err := formatter.Write(w, finalResult, meta); err != nil {
			return fmt.Errorf("writing output: %w", err)
		}

		return nil
	},
}

// Execute runs the root command.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "v0x:", err)
		os.Exit(1)
	}
}

func init() {
	flags := rootCmd.PersistentFlags()

	flags.StringVar(&cfg.URL, "url", "", "Target URL to crawl (required)")
	flags.IntVar(&cfg.Depth, "depth", 2, "Max crawl depth")
	flags.IntVar(&cfg.MinWordLength, "min-word-length", 3, "Minimum word length to collect")
	flags.StringVar(&cfg.UserAgent, "user-agent", "v0x/1.0", "Custom User-Agent string")
	flags.StringVar(&cfg.Output, "output", "", "Output file path (default: stdout)")
	flags.StringVar(&cfg.Format, "format", "txt", "Output format: txt, json, csv, md")
	flags.BoolVar(&cfg.Headless, "headless", true, "Use headless browser (playwright-go)")
	flags.BoolVar(&noHeadless, "no-headless", false, "Disable headless, use net/http instead")
	flags.IntVar(&cfg.Delay, "delay", 500, "Delay in ms between requests")
	flags.BoolVar(&cfg.Verbose, "verbose", false, "Verbose logging")
	flags.DurationVar(&cfg.Timeout, "timeout", 5*time.Minute, "Max crawl duration (0 = unlimited)")

	// Form-based login (playwright-only)
	flags.StringVar(&cfg.AuthFormURL, "auth-form-url", "", "URL of the login form page")
	flags.StringVar(&cfg.AuthFormUser, "auth-form-user", "", "Username to submit in the login form")
	flags.StringVar(&cfg.AuthFormPass, "auth-form-pass", "", "Password to submit in the login form")
	flags.StringVar(&cfg.AuthFormUserField, "auth-form-user-field", "username", "Name attribute of the username input")
	flags.StringVar(&cfg.AuthFormPassField, "auth-form-pass-field", "password", "Name attribute of the password input")
	flags.StringVar(&cfg.AuthFormSubmit, "auth-form-submit", "[type=submit]", "CSS selector for the submit button")
	flags.StringVar(&cfg.AuthVerifySelector, "auth-verify-selector", "", "CSS selector that must be present after login to confirm authentication succeeded")

	// HTTP Basic auth
	flags.StringVar(&cfg.AuthBasicUser, "auth-basic-user", "", "HTTP Basic auth username")
	flags.StringVar(&cfg.AuthBasicPass, "auth-basic-pass", "", "HTTP Basic auth password")

	// Cookie injection
	flags.StringVar(&cfg.AuthCookie, "auth-cookie", "", `Cookie string to inject, e.g. "session=abc; token=xyz"`)

	// Bearer token / custom header
	flags.StringVar(&cfg.AuthBearer, "auth-bearer", "", "Bearer token for Authorization header")
	flags.StringVar(&cfg.AuthHeader, "auth-header", "", `Custom auth header in "Name: Value" format`)
}
