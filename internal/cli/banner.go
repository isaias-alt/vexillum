package cli

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/isaias-alt/vexillum/internal/banner"
)

const bannerUsage = `Publish an HTML artifact to a public URL, or update one already published.

Usage:
  vexillum banner <file.html> [--private | --password <pw>]
  vexillum banner <file.html> --site <id> --update-key <key> [--private | --password <pw>]
  vexillum banner --unpublish --site <id> --update-key <key>

The first form publishes a brand-new page: local assets (images, CSS, JS
in the same directory tree as <file.html>) are inlined into the HTML as
data: URIs, remote references (http/https, a CDN, Google Fonts) are left
as-is, and the result is POSTed to a third-party HTML hosting service
(ht-ml.app by default) - no account or API key needed. The response
carries the page's URL and an update_key.

update_key is the ONLY credential that can ever touch that page again -
it is printed once, right after publishing, and vexillum never stores
it anywhere. Save it yourself, right now. If you lose it, that page can
never be updated or unpublished again, by anyone, for any reason - there
is no "forgot my key" recovery.

If the publish request fails ambiguously (a timeout or a 5xx - anything
where it's unclear whether the server actually created the page), treat
the update_key as lost even though nothing was printed: retrying is NOT
safe, because every POST creates a brand-new page rather than confirming
an earlier one. If that happens, this command tells you explicitly.

The second form republishes an existing page in place: pass the same
--site and --update-key you were given when it was first published,
along with the new file to publish there.

--unpublish replaces the page at --site with a placeholder page behind a
freshly-generated password that is discarded immediately after the
request (nobody, including you, will ever see it) - there is no real
delete: the URL keeps responding, it just shows the placeholder. Running
this command again with the same --site and --update-key (an ordinary
republish) makes the original content visible there again at any time.

Password flags (only meaningful when publishing or republishing content,
not with --unpublish):

  --private        generate a page password locally and print it once
  --password <pw>   gate the page with a password you already chose

Either sets the page's password in the same request that publishes it.
There is no way to make an already-private page public again - the
backend silently ignores an attempt to clear a password, so this command
doesn't offer a flag that would misreport a page as public while it
stays gated.
`

// Banner runs the "vexillum banner" command.
func Banner(args []string) int {
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help") {
		fmt.Print(bannerUsage)
		return 0
	}

	opts, err := parseBannerArgs(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, "vexillum:", err)
		fmt.Fprint(os.Stderr, bannerUsage)
		return 1
	}

	return runBanner(opts, banner.NewClient(), os.Stdout, os.Stderr)
}

type bannerArgs struct {
	file      string
	private   bool
	password  string
	site      string
	updateKey string
	unpublish bool
}

func parseBannerArgs(args []string) (bannerArgs, error) {
	var a bannerArgs
	var files []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--private":
			a.private = true
		case "--password":
			if i+1 >= len(args) {
				return bannerArgs{}, fmt.Errorf("--password requires a value")
			}
			i++
			a.password = args[i]
		case "--site":
			if i+1 >= len(args) {
				return bannerArgs{}, fmt.Errorf("--site requires a value")
			}
			i++
			a.site = args[i]
		case "--update-key":
			if i+1 >= len(args) {
				return bannerArgs{}, fmt.Errorf("--update-key requires a value")
			}
			i++
			a.updateKey = args[i]
		case "--unpublish":
			a.unpublish = true
		default:
			if len(args[i]) > 0 && args[i][0] == '-' {
				return bannerArgs{}, fmt.Errorf("unknown banner flag %q", args[i])
			}
			files = append(files, args[i])
		}
	}

	if len(files) > 1 {
		return bannerArgs{}, fmt.Errorf("expected a single HTML file, got %d", len(files))
	}
	if len(files) == 1 {
		a.file = files[0]
	}

	if a.private && a.password != "" {
		return bannerArgs{}, fmt.Errorf("--private and --password are mutually exclusive")
	}
	if (a.site == "") != (a.updateKey == "") {
		return bannerArgs{}, fmt.Errorf("--site and --update-key must be passed together")
	}
	if a.unpublish {
		if a.site == "" {
			return bannerArgs{}, fmt.Errorf("--unpublish requires --site and --update-key")
		}
		if a.file != "" {
			return bannerArgs{}, fmt.Errorf("--unpublish takes no file - it publishes a placeholder, not %q", a.file)
		}
		if a.private || a.password != "" {
			return bannerArgs{}, fmt.Errorf("--unpublish generates and discards its own password - it can't be combined with --private or --password")
		}
	} else if a.file == "" {
		return bannerArgs{}, fmt.Errorf("missing file to publish")
	}

	return a, nil
}

func runBanner(opts bannerArgs, client *banner.Client, stdout, stderr io.Writer) int {
	if opts.unpublish {
		return runBannerUnpublish(opts, client, stdout, stderr)
	}
	if opts.site != "" {
		return runBannerRepublish(opts, client, stdout, stderr)
	}
	return runBannerPublish(opts, client, stdout, stderr)
}

func runBannerPublish(opts bannerArgs, client *banner.Client, stdout, stderr io.Writer) int {
	html, warnings, err := banner.InlineLocalAssets(opts.file)
	if err != nil {
		fmt.Fprintf(stderr, "vexillum: %v\n", err)
		return 1
	}
	printWarnings(stderr, warnings)

	password, shown, err := resolveBannerPassword(opts)
	if err != nil {
		fmt.Fprintf(stderr, "vexillum: %v\n", err)
		return 1
	}

	site, err := client.Create(html, password)
	if err != nil {
		reportBannerCreateError(stderr, err)
		return 1
	}

	fmt.Fprintf(stdout, "published: %s\n", site.URL)
	if site.SiteID != "" {
		fmt.Fprintf(stdout, "site_id:   %s\n", site.SiteID)
	}
	fmt.Fprintln(stdout)
	fmt.Fprintf(stdout, "update_key: %s\n", site.UpdateKey)
	fmt.Fprintln(stdout, "This is the ONLY credential that can update or unpublish this page -")
	fmt.Fprintln(stdout, "it is shown once, right now, and vexillum does not store it anywhere.")
	fmt.Fprintln(stdout, "Save it yourself. Lose it and this page can never be touched again.")
	if site.SiteIDRejected {
		fmt.Fprintln(stdout)
		fmt.Fprintln(stdout, "WARNING: the host returned a site_id vexillum can't safely use, and --site")
		fmt.Fprintln(stdout, "is half the republish credential. This page can NEVER be republished or")
		fmt.Fprintln(stdout, "unpublished, even though its update_key is printed above.")
	}

	if shown {
		fmt.Fprintln(stdout)
		fmt.Fprintf(stdout, "password: %s\n", password)
		fmt.Fprintln(stdout, "Shown once, right now - vexillum does not store it. It's a shared")
		fmt.Fprintln(stdout, "secret: anyone you give it to can open this page.")
	}
	return 0
}

func runBannerRepublish(opts bannerArgs, client *banner.Client, stdout, stderr io.Writer) int {
	html, warnings, err := banner.InlineLocalAssets(opts.file)
	if err != nil {
		fmt.Fprintf(stderr, "vexillum: %v\n", err)
		return 1
	}
	printWarnings(stderr, warnings)

	password, shown, err := resolveBannerPassword(opts)
	if err != nil {
		fmt.Fprintf(stderr, "vexillum: %v\n", err)
		return 1
	}

	site, err := client.Update(opts.site, opts.updateKey, html, password)
	if err != nil {
		fmt.Fprintf(stderr, "vexillum: %v\n", err)
		return 1
	}

	if site.URL != "" {
		fmt.Fprintf(stdout, "republished: %s\n", site.URL)
	} else {
		fmt.Fprintf(stdout, "republished site %s (server didn't return a URL - it should be unchanged from the original publish)\n", opts.site)
	}
	if shown {
		fmt.Fprintln(stdout)
		fmt.Fprintf(stdout, "password: %s\n", password)
		fmt.Fprintln(stdout, "Shown once, right now - vexillum does not store it. It's a shared")
		fmt.Fprintln(stdout, "secret: anyone you give it to can open this page.")
	}
	return 0
}

func runBannerUnpublish(opts bannerArgs, client *banner.Client, stdout, stderr io.Writer) int {
	discardPassword, err := banner.GeneratePassword()
	if err != nil {
		fmt.Fprintf(stderr, "vexillum: %v\n", err)
		return 1
	}

	site, err := client.Update(opts.site, opts.updateKey, banner.UnpublishPlaceholderHTML, discardPassword)
	if err != nil {
		fmt.Fprintf(stderr, "vexillum: %v\n", err)
		return 1
	}

	id := opts.site
	if site.SiteID != "" {
		id = site.SiteID
	}
	fmt.Fprintf(stdout, "unpublished site %s\n", id)
	fmt.Fprintln(stdout, "The URL is still live and now shows a placeholder page behind a password")
	fmt.Fprintln(stdout, "that was generated and discarded immediately - nobody can open it,")
	fmt.Fprintln(stdout, "including you. There is no real delete. Republishing this site_id with")
	fmt.Fprintln(stdout, "the same update_key makes your content visible there again.")
	return 0
}

// resolveBannerPassword decides the password (if any) to send with a
// publish or republish request. shown reports whether it should be
// printed to the user - true only for a freshly-generated --private
// password, never for one the user already supplied with --password.
func resolveBannerPassword(opts bannerArgs) (password string, shown bool, err error) {
	if opts.private {
		password, err = banner.GeneratePassword()
		if err != nil {
			return "", false, err
		}
		return password, true, nil
	}
	return opts.password, false, nil
}

func printWarnings(stderr io.Writer, warnings []string) {
	for _, w := range warnings {
		fmt.Fprintf(stderr, "vexillum: %s\n", w)
	}
}

func reportBannerCreateError(stderr io.Writer, err error) {
	fmt.Fprintf(stderr, "vexillum: %v\n", err)
	var ambiguous *banner.AmbiguousCreateError
	if !errors.As(err, &ambiguous) {
		return
	}
	fmt.Fprintln(stderr)
	fmt.Fprintln(stderr, "If the page was in fact created despite this error, its update_key was")
	fmt.Fprintln(stderr, "in a response this command never received or couldn't parse - it is lost")
	fmt.Fprintln(stderr, "forever, with no way to recover it. Do not retry assuming this is safe:")
	fmt.Fprintln(stderr, "every attempt creates a brand-new page, so a retry risks leaving an")
	fmt.Fprintln(stderr, "orphaned, unmanageable page live indefinitely.")
}
