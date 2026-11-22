package fuse

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Transpiled from src/openvfsfuse/openvfsfuse.h (openVFSfuse_Args) and
// src/openvfsfuse/main.cpp.

//go:embed config.json
var defaultConfig []byte

const maxFuseArgs = 32

// Config field names, matching main.cpp's ConfigXxxStr constants. Go struct
// tags have to be literals, so they are repeated in Config below.
const (
	configIgnoreAppsStr              = "ignoreApps"
	configByNameStr                  = "byName"
	configEndsWith                   = "endsWith"
	configHydrationTimeoutSecondsStr = "hydrationTimeoutSeconds"
)

// Config is the on-disk shape of the -i file. Every setting is optional so that
// a config file written for an older version keeps working.
type Config struct {
	IgnoreApps struct {
		ByName   []string `json:"byName"`
		EndsWith []string `json:"endsWith"`
	} `json:"ignoreApps"`
	HydrationTimeoutSeconds int `json:"hydrationTimeoutSeconds"`
}

func parseConfig(b []byte) (Config, error) {
	var cfg Config
	err := json.Unmarshal(b, &cfg)
	return cfg, err
}

// Args is openVFSfuse_Args.
type Args struct {
	MountPoint            string // where the users read files
	Owner                 string // an identifier used during the re
	SocketPath            string // default value for debugging
	IsDaemon              bool   // true == spawn in background
	FuseArgv              []string
	AppsNoHydrateFull     []string // these apps are not permitted to cause a dehydration
	AppsNoHydrateEndsWith []string
	// HydrationTimeout is the upper bound for how long open() blocks waiting
	// for the desktop client to hydrate a file. A large file over a slow link
	// is legitimately slow, an unresponsive client is not -- so this is
	// configurable.
	HydrationTimeout time.Duration
	DebugEnabled     bool // checked in the central logging function if logging is enabled
}

// NewArgs returns openVFSfuse_Args with its default member initialisers.
func NewArgs() Args {
	return Args{
		SocketPath:       "/run/user/1000/OpenCloud/socket",
		IsDaemon:         true,
		HydrationTimeout: 300 * time.Second,
	}
}

func usage(w io.Writer, name string) {
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, name+" [-h] | [-f] [-p] [-d] -i config-file -o ownerId /directory-mountpoint")
	fmt.Fprintln(w, "Type 'man openvfsfuse' for more details")
}

// ProcessArgs is processArgs(). argv[0] is the program name, as in C.
// The second return value is false where upstream returns an empty optional.
func ProcessArgs(argv []string, stdout, stderr io.Writer) (Args, bool) {
	out := NewArgs()
	if len(argv) == 0 {
		return out, false
	}
	// pass executable name through
	out.FuseArgv = append(out.FuseArgv, argv[0])

	// preset passed standard options which can not be set in fuse_config in
	// the init function called openVFSfuse_init()
	// auto_umount: Unmount the fuse layer automatically if the app crashes.
	opts := []string{"auto_unmount"}

	options, operands, ok := getopt(argv[1:], "hpfdi:o:s:", stderr)
	if !ok {
		return out, false
	}

	for _, opt := range options {
		switch opt.name {
		case 'h':
			usage(stderr, argv[0])
			return out, false
		case 'f':
			out.IsDaemon = false
			// this option was added in fuse 2.x
			out.FuseArgv = append(out.FuseArgv, "-f")
			fmt.Fprintln(stdout, "openVFSfuse not running as a daemon")
		case 'p':
			// make the mount public
			opts = append(opts, "allow_other", "default_permission")

			fmt.Fprintln(stdout, "openVFSfuse running as a public filesystem")
		case 'd':
			// enable debug logging, implies -f
			out.FuseArgv = append(out.FuseArgv, "-d")
			out.DebugEnabled = true
			fmt.Fprintln(stdout, "openVFSfuse running with debug log enabled")
		case 'i':
			b, err := os.ReadFile(opt.arg)
			if err != nil {
				fmt.Fprintln(stderr, "Failed to open config file "+opt.arg)
				return out, false
			}
			data, err := parseConfig(b)
			if err != nil {
				fmt.Fprintf(stderr, "Failed to parse config file %s: %v\n", opt.arg, err)
				return out, false
			}

			out.AppsNoHydrateFull = data.IgnoreApps.ByName
			out.AppsNoHydrateEndsWith = data.IgnoreApps.EndsWith

			timeoutSeconds := data.HydrationTimeoutSeconds
			if timeoutSeconds > 0 {
				out.HydrationTimeout = time.Duration(timeoutSeconds) * time.Second
			} else if timeoutSeconds < 0 {
				fmt.Fprintln(stderr, configHydrationTimeoutSecondsStr+" must be positive, keeping the default")
			}
		case 'o':
			out.Owner = opt.arg
		case 's':
			out.SocketPath = opt.arg
		}
	}

	if len(opts) > 0 {
		out.FuseArgv = append(out.FuseArgv, "-o")
		out.FuseArgv = append(out.FuseArgv, strJoin(opts, ','))
	}

	if len(operands) >= 1 {
		// std::filesystem::canonical: resolve symlinks, require existence.
		canonical, err := filepath.EvalSymlinks(operands[0])
		if err == nil {
			canonical, err = filepath.Abs(canonical)
		}
		if err != nil {
			fmt.Fprintf(stderr, "Failed to resolve mountpoint %s: %v\n", operands[0], err)
			return out, false
		}
		out.MountPoint = canonical
		out.FuseArgv = append(out.FuseArgv, out.MountPoint)
		operands = operands[1:]
	} else {
		fmt.Fprintln(stderr, "Missing mountpoint")
		usage(stderr, argv[0])
		return out, false
	}

	// If there are still extra unparsed arguments, pass them onto FUSE..
	for _, extra := range operands {
		if len(out.FuseArgv) >= maxFuseArgs {
			// upstream asserts
			break
		}
		out.FuseArgv = append(out.FuseArgv, extra)
	}

	if !filepath.IsAbs(out.MountPoint) {
		fmt.Fprintln(stderr, "You must use absolute paths (beginning with '/') for "+out.MountPoint)
		return out, false
	}

	return out, true
}

// BlockedToOpen is VFSFuseContext::blockedToOpen().
func (a Args) BlockedToOpen(app string) bool {
	for _, name := range a.AppsNoHydrateFull {
		if app == name {
			return true
		}
	}
	for _, suffix := range a.AppsNoHydrateEndsWith {
		if strings.HasSuffix(app, suffix) {
			return true
		}
	}
	return false
}

// parsedOption is one option getopt(3) returned, in the order it appeared.
type parsedOption struct {
	name byte
	arg  string
}

// getopt is getopt(3) restricted to what main.cpp needs: short options, an
// optional ':' in optstring for the ones that take an argument, and the GNU
// behaviour of permuting operands to the end so that options may follow the
// mountpoint. Go's flag package stops at the first operand, which would reject
// command lines upstream accepts.
//
// opterr is 0 upstream, so an unrecognised option is reported by getopt as '?'
// and lands in a default branch that only asserts; a release build therefore
// skips it, which is what happens here.
func getopt(args []string, optstring string, stderr io.Writer) ([]parsedOption, []string, bool) {
	takesArg := func(c byte) (known, arg bool) {
		for i := 0; i < len(optstring); i++ {
			if optstring[i] == c && c != ':' {
				return true, i+1 < len(optstring) && optstring[i+1] == ':'
			}
		}
		return false, false
	}

	var options []parsedOption
	var operands []string

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			operands = append(operands, args[i+1:]...)
			break
		}
		if len(arg) < 2 || arg[0] != '-' {
			operands = append(operands, arg)
			continue
		}
		for j := 1; j < len(arg); j++ {
			c := arg[j]
			known, wantsArg := takesArg(c)
			if !known {
				fmt.Fprintf(stderr, "invalid option -- '%c'\n", c)
				continue
			}
			if !wantsArg {
				options = append(options, parsedOption{name: c})
				continue
			}
			// The rest of this element is the argument, else the next element.
			if j+1 < len(arg) {
				options = append(options, parsedOption{name: c, arg: arg[j+1:]})
			} else if i+1 < len(args) {
				i++
				options = append(options, parsedOption{name: c, arg: args[i]})
			} else {
				fmt.Fprintf(stderr, "option requires an argument -- '%c'\n", c)
			}
			break
		}
	}

	return options, operands, true
}
