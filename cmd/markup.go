package cmd

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/Bnei-Baruch/archive-stt/batch"
	"github.com/Bnei-Baruch/archive-stt/common"
)

const publicURL = "https://ai-tools.kab.info/archive-stt"

var markupFlags struct {
	language, ssh, url, ok, failed string
	force, dryRun, noSSH           bool
	rebuild, redoSTT               bool
	parallel, port                 int
	poll                           time.Duration
}

var markupCmd = &cobra.Command{
	Use:   "markup <uid | file | -> ...",
	Short: "Start markup runs on the server for a list of lessons, a few at a time, and wait for them",
	Long: `Start markup runs on the server for a list of lessons, a few at a time, and wait for them.

    archive-stt markup 5JBUHZoo                 # one lesson
    archive-stt markup clips.txt                # a file: a uid per line (after a TAB -- anything, # -- a comment)
    ... | archive-stt markup -                  # stdin
    archive-stt markup --dry-run clips.txt      # what would be started; nothing is started or written

The service takes a run only from its own host, so an SSH tunnel to it is opened here (--ssh or markup.ssh in
the config / ARCHIVE_STT_MARKUP_SSH, e.g. root@<ai-tools>; VPN needed): local --port -> the server's
127.0.0.1:8770. No keys. With --no-ssh, --url is called as it is (on the server itself, or a local service).

A lesson in the ok list (--ok) is skipped without asking the server. On the server: ready -> listed as ok, not run
again; pending -> waited for. --force runs them all again. A start answered 404 (no such unit), 422 (no audio)
or else goes to the failed list (--failed, uid<TAB>error); a uid leaves it when a later run passes.
--parallel runs are kept in flight (default 1: the GPU is shared with ai-trns's other jobs and the live
broadcast). --redo-stt runs again the ready lessons whose words are from the STT: the server takes the lesson's
checked subtitles (MDB, made before 2027) where it has them -- give it the lessons that have them.
--rebuild starts no runs: each lesson's segmentation is rebuilt on the server from its kept words and
diarization and its transcript in kmedia now, no ai-trns (lists: rebuild-ok.txt, rebuild-failed.txt).
Ctrl+C stops starting new ones and waits for the started; a second Ctrl+C leaves them running on the
server (a later batch picks them up). At the end: this batch's ok and failed uids; exit code 1 if any failed.`,
	Args: cobra.MinimumNArgs(1),
	Run:  markupFn,
}

func init() {
	RootCmd.AddCommand(markupCmd)
	f := markupCmd.Flags()
	f.StringVar(&markupFlags.language, "language", "", "the audio's language (default: the lesson's original)")
	f.BoolVar(&markupFlags.force, "force", false, "run again even if the markup is ready")
	f.BoolVar(&markupFlags.redoSTT, "redo-stt", false, "run again the ready lessons whose words are from the STT")
	f.BoolVar(&markupFlags.rebuild, "rebuild", false, "rebuild the segmentation from the kept words, no runs")
	f.IntVar(&markupFlags.parallel, "parallel", 1, "runs in flight at once")
	f.DurationVar(&markupFlags.poll, "poll", 20*time.Second, "between status checks")
	f.BoolVar(&markupFlags.dryRun, "dry-run", false, "only show what would be started")
	f.StringVar(&markupFlags.ssh, "ssh", "", "the server for the tunnel, user@host (default: markup.ssh)")
	f.IntVar(&markupFlags.port, "port", 8771, "the local end of the tunnel")
	f.BoolVar(&markupFlags.noSSH, "no-ssh", false, "no tunnel: call --url directly")
	f.StringVar(&markupFlags.url, "url", "", "the service (default: the tunnel, http://127.0.0.1:PORT)")
	f.StringVar(&markupFlags.ok, "ok", "", "lessons done: a uid per line, appended (default: markup-ok.txt, "+
		"rebuild-ok.txt with --rebuild)")
	f.StringVar(&markupFlags.failed, "failed", "", "lessons that failed: uid<TAB>error (default: markup-failed.txt, "+
		"rebuild-failed.txt with --rebuild)")
	viper.SetDefault("markup.ssh", "")
}

func fatal(format string, a ...interface{}) {
	fmt.Fprintf(os.Stderr, format+"\n", a...)
	os.Exit(2)
}

// openTunnel runs ssh -N -L port:127.0.0.1:8770 server and waits until the service answers through it
func openTunnel(server string, port int, base string, client *http.Client) *exec.Cmd {
	ssh := exec.Command("ssh", "-N", "-o", "BatchMode=yes", "-o", "ExitOnForwardFailure=yes",
		"-o", "ServerAliveInterval=30", "-L", fmt.Sprintf("%d:127.0.0.1:8770", port), server)
	ssh.Stderr = os.Stderr
	// its own process group: Ctrl+C in the terminal goes to this program, not to the tunnel
	ssh.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := ssh.Start(); err != nil {
		fatal("ssh: %v", err)
	}
	exited := make(chan struct{})
	go func() { ssh.Wait(); close(exited) }()
	for i := 0; i < 30; i++ {
		select {
		case <-exited:
			fatal("ssh tunnel to %s failed (VPN? key?)", server)
		default:
		}
		if res, err := client.Get(base + "/health"); err == nil {
			res.Body.Close()
			if res.StatusCode == http.StatusOK {
				return ssh
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	ssh.Process.Kill()
	fatal("ssh tunnel: archive-stt does not answer")
	return nil
}

func markupFn(cmd *cobra.Command, args []string) {
	fl := markupFlags
	if fl.language != "" && !common.IsLanguage(fl.language) {
		fatal("--language: one of %s", strings.Join(common.Languages, ", "))
	}
	if fl.rebuild && (fl.force || fl.redoSTT || fl.language != "") {
		fatal("--rebuild: without --force, --redo-stt and --language")
	}
	list := "markup"
	if fl.rebuild {
		list = "rebuild"
	}
	if fl.ok == "" {
		fl.ok = list + "-ok.txt"
	}
	if fl.failed == "" {
		fl.failed = list + "-failed.txt"
	}
	if fl.parallel < 1 {
		fatal("--parallel: 1 or more")
	}
	uids, err := batch.ReadUIDs(args, os.Stdin)
	if err != nil {
		fatal("%v", err)
	}
	if len(uids) == 0 {
		fatal("no uids")
	}
	base := strings.TrimRight(fl.url, "/")
	if base == "" {
		base = fmt.Sprintf("http://127.0.0.1:%d", fl.port)
	}
	client := &http.Client{Timeout: 30 * time.Second}

	stop, abort := make(chan struct{}), make(chan struct{})
	r := &batch.Runner{Base: base, StatusBase: base, Language: fl.language, Force: fl.force,
		RedoSTT: fl.redoSTT, Rebuild: fl.rebuild, Parallel: fl.parallel,
		Poll: fl.poll, DryRun: fl.dryRun, OKPath: fl.ok, FailedPath: fl.failed, Out: os.Stdout, HTTP: client,
		Stop: stop, Abort: abort}
	todo, err := r.Prepare(uids)
	if err != nil {
		fatal("%v", err)
	}
	language := fl.language
	if language == "" {
		language = "original"
	}
	fmt.Printf("%s %d lessons: %d in %s already, %d to check and run; %d at a time, language %s\n",
		time.Now().Format("15:04:05"), len(uids), len(r.Skipped), fl.ok, len(todo), fl.parallel, language)
	if len(todo) == 0 {
		fmt.Println("nothing to do")
		return
	}

	var tunnel *exec.Cmd
	// os.Exit skips defers: the tunnel (in its own process group) is closed here
	exit := func(code int) {
		if tunnel != nil {
			tunnel.Process.Kill()
		}
		os.Exit(code)
	}
	switch {
	case fl.dryRun && !fl.noSSH:
		r.StatusBase = publicURL // a dry run only reads: the public proxy will do
	case !fl.noSSH:
		server := fl.ssh
		if server == "" {
			server = viper.GetString("markup.ssh")
		}
		if server == "" {
			fatal("the server for the tunnel: --ssh user@host, markup.ssh in config.toml or ARCHIVE_STT_MARKUP_SSH " +
				"(or --no-ssh --url ...)")
		}
		tunnel = openTunnel(server, fl.port, base, client)
	}

	signals := make(chan os.Signal, 2)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-signals
		close(stop)
		<-signals
		close(abort)
	}()

	runErr := r.Run(todo)
	r.Summary()
	if runErr != nil {
		fmt.Fprintln(os.Stderr, runErr)
		exit(2)
	}
	if len(r.Failed) > 0 {
		exit(1)
	}
	exit(0)
}
