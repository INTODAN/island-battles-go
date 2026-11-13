package islandbattles

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
)

// Main runs the bot program's command line, and exits with its status. Call
// it from your main function:
//
//	func main() {
//		islandbattles.Main(play)
//	}
//
// The program then has these subcommands:
//
//	serve    serve the bot on 127.0.0.1:41414: it takes turn deliveries and submits its answers
//	check    call the bot once on a fixture turn, and report what is wrong
//	fixture  print the fixture turn as JSON, for check --turn
//	run      play a seat of a lobby's game without a browser
//	worker   play turns for the server's runners, as JSON Lines on stdin and stdout
//
// Run the program with -h, or a subcommand with -h, for their flags. Go
// doesn't reload code: after changing the bot, stop serve and start it again.
func Main(play Play) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	status := runMain(ctx, invocation{name: filepath.Base(os.Args[0]), play: play, stdout: os.Stdout, stderr: os.Stderr, stdin: os.Stdin}, os.Args[1:])
	stop()
	os.Exit(status)
}

// invocation is one run of a bot program's command line.
type invocation struct {
	// name is the program's name, which serve reports on /health.
	name   string
	play   Play
	stdout io.Writer
	stderr io.Writer
	// stdin is what worker reads its requests from; nil means os.Stdin.
	stdin io.Reader
	// clientOptions are added to run's Client options; tests set them.
	clientOptions []ClientOption
	// lookupEnv reads the environment; nil means os.LookupEnv.
	lookupEnv func(string) (string, bool)
}

// subcommand is one subcommand of Main. run gets the arguments after the
// subcommand's name and returns the exit status.
type subcommand struct {
	name    string
	summary string
	run     func(ctx context.Context, program invocation, arguments []string) int
}

// subcommands are Main's subcommands, in the order the usage lists them.
func subcommands() []subcommand {
	return []subcommand{
		{name: "serve", summary: fmt.Sprintf("serve the bot on %s:%d: it takes turn deliveries and submits its answers", serveHost, DefaultPort), run: serve},
		{name: "check", summary: "call the bot once on a fixture turn, and report what is wrong", run: check},
		{name: "fixture", summary: "print the fixture turn as JSON, for check --turn", run: printFixture},
		{name: "run", summary: "play a seat of a lobby's game without a browser", run: runSeat},
		{name: "worker", summary: "play turns for the server's runners, as JSON Lines on stdin and stdout", run: worker},
	}
}

// runMain runs the command line arguments and returns the exit status: 0 on
// success, 1 when the subcommand failed, 2 for a usage error.
func runMain(ctx context.Context, program invocation, arguments []string) int {
	if len(arguments) == 0 {
		usage(program)
		return 2
	}
	for _, command := range subcommands() {
		if command.name == arguments[0] {
			return command.run(ctx, program, arguments[1:])
		}
	}
	if arguments[0] == "-h" || arguments[0] == "--help" || arguments[0] == "help" {
		usage(program)
		return 0
	}
	_, _ = fmt.Fprintf(program.stderr, "%s: unknown command %q\n", program.name, arguments[0])
	usage(program)
	return 2
}

func usage(program invocation) {
	_, _ = fmt.Fprintf(program.stderr, "Usage: %s <command> [flags]\n\nCommands:\n", program.name)
	for _, command := range subcommands() {
		_, _ = fmt.Fprintf(program.stderr, "  %-8s %s\n", command.name, command.summary)
	}
	_, _ = fmt.Fprintf(program.stderr, "\nRun %s <command> -h for a command's flags.\n", program.name)
}

// newFlags is a subcommand's flag set, which prints its errors and usage to
// the program's stderr.
func newFlags(program invocation, name, description string) *flag.FlagSet {
	flags := flag.NewFlagSet(program.name+" "+name, flag.ContinueOnError)
	flags.SetOutput(program.stderr)
	flags.Usage = func() {
		_, _ = fmt.Fprintf(program.stderr, "Usage: %s %s [flags]\n\n%s\n\nFlags:\n", program.name, name, description)
		flags.PrintDefaults()
	}
	return flags
}

// parseFlags parses a subcommand's flags. It returns the exit status and
// false when the subcommand must stop: 0 after -h, 2 for a usage error.
func parseFlags(flags *flag.FlagSet, arguments []string) (int, bool) {
	if err := flags.Parse(arguments); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0, false
		}
		return 2, false
	}
	if flags.NArg() > 0 {
		_, _ = fmt.Fprintf(flags.Output(), "unexpected arguments: %v\n", flags.Args())
		flags.Usage()
		return 2, false
	}
	return 0, true
}

func printFixture(_ context.Context, program invocation, arguments []string) int {
	flags := newFlags(program, "fixture", "Prints the fixture turn that check plays, as JSON, for check --turn.")
	if status, ok := parseFlags(flags, arguments); !ok {
		return status
	}
	if _, err := program.stdout.Write(fixtureTurnJSON); err != nil {
		return 1
	}
	return 0
}
