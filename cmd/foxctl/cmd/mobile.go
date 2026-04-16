package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/joshka0/foxctl/cmd/foxctl/cmd/memorycmd"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/protocol"
	"github.com/joshka0/foxctl/internal/tooling/shellreduce"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type mobilePlatform string

const (
	mobileIOS     mobilePlatform = "ios"
	mobileAndroid mobilePlatform = "android"
)

type mobileTimingFlags struct {
	preDelay  float64
	postDelay float64
}

type mobileTargetFlags struct {
	udid         string
	serial       string
	screenWidth  int
	screenHeight int
	timing       mobileTimingFlags
}

type mobileTapFlags struct {
	x     int
	y     int
	id    string
	label string
}

type mobileSwipeFlags struct {
	startX   int
	startY   int
	endX     int
	endY     int
	duration float64
}

type mobileEnvelope struct {
	Version int            `json:"version"`
	Status  string         `json:"status"`
	Command string         `json:"command"`
	Data    map[string]any `json:"data"`
	Error   map[string]any `json:"error"`
}

type mobileBatchState struct {
	axCache map[mobilePlatform][]any
}

func newMobileCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mobile",
		Short: "Drive iOS simulators and Android emulators",
		Long: `Drive iOS simulators and Android emulators using the existing mobile skills.

The low-level skills still emit JSON envelopes. These subcommands provide a
human-friendly CLI for common mobile QA operations.`,
		Example: `  foxctl mobile list-simulators
  foxctl mobile ios tap -x 100 -y 200 --udid "$UDID"
  foxctl mobile ios tap --label Safari --udid "$UDID"
  foxctl mobile android type 'Hello World!' --serial emulator-5554
  foxctl mobile ios gesture scroll-up --pre-delay 0.5 --post-delay 1`,
	}

	cmd.AddCommand(
		newMobileListSimulatorsCommand(),
		newMobilePlatformCommand(mobileIOS),
		newMobilePlatformCommand(mobileAndroid),
	)
	return cmd
}

func init() {
	rootCmd.AddCommand(newMobileCommand())
}

func newMobileListSimulatorsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "list-simulators",
		Aliases: []string{"list-devices", "devices"},
		Short:   "List available iOS simulators and Android emulators",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runMobileSkill(cmd, "mobile/unified", map[string]any{
				"operation": "list_devices",
			}, mobileTimingFlags{})
		},
	}
	return cmd
}

func newMobilePlatformCommand(platform mobilePlatform) *cobra.Command {
	label := "iOS Simulator"
	if platform == mobileAndroid {
		label = "Android Emulator"
	}

	cmd := &cobra.Command{
		Use:   string(platform),
		Short: "Control " + label,
	}

	cmd.AddCommand(
		newMobilePlatformListCommand(platform),
		newMobileTapCommand(platform),
		newMobileSwipeCommand(platform),
		newMobileGestureCommand(platform),
		newMobileTypeCommand(platform),
		newMobileButtonCommand(platform),
		newMobileLaunchCommand(platform),
		newMobileTerminateCommand(platform),
		newMobileOpenURLCommand(platform),
		newMobileScreenshotCommand(platform),
		newMobileDescribeUICommand(platform),
		newMobileBatchCommand(platform),
	)
	if platform == mobileAndroid {
		cmd.AddCommand(newMobileKeyCommand(platform))
	}

	return cmd
}

func newMobilePlatformListCommand(platform mobilePlatform) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "list-simulators",
		Aliases: []string{"list-devices", "devices"},
		Short:   "List available targets for this platform",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runMobileSkill(cmd, mobileSkillName(platform), map[string]any{
				"operation": "list_devices",
			}, mobileTimingFlags{})
		},
	}
	return cmd
}

func newMobileTapCommand(platform mobilePlatform) *cobra.Command {
	var target mobileTargetFlags
	var tap mobileTapFlags

	cmd := &cobra.Command{
		Use:   "tap",
		Short: "Tap coordinates or an accessibility element",
		Example: `  foxctl mobile ios tap -x 100 -y 200 --udid "$UDID"
  foxctl mobile ios tap --label Safari --udid "$UDID"
  foxctl mobile android tap --id com.example:id/search --serial emulator-5554`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			payload, err := mobileTapPayload(cmd, platform, target, tap, nil)
			if err != nil {
				return err
			}
			return runMobileSkill(cmd, mobileSkillName(platform), payload, target.timing)
		},
	}

	addMobileTargetFlags(cmd, platform, &target)
	addMobileTimingFlags(cmd, &target.timing)
	cmd.Flags().IntVarP(&tap.x, "x", "x", 0, "X coordinate")
	cmd.Flags().IntVarP(&tap.y, "y", "y", 0, "Y coordinate")
	cmd.Flags().StringVar(&tap.id, "id", "", "Accessibility identifier or resource ID to tap")
	cmd.Flags().StringVar(&tap.label, "label", "", "Accessibility label, text, or content description to tap")
	return cmd
}

func newMobileSwipeCommand(platform mobilePlatform) *cobra.Command {
	var target mobileTargetFlags
	var swipe mobileSwipeFlags

	cmd := &cobra.Command{
		Use:   "swipe",
		Short: "Swipe between two coordinates",
		RunE: func(cmd *cobra.Command, _ []string) error {
			payload, err := mobileSwipePayload(cmd, platform, target, swipe)
			if err != nil {
				return err
			}
			return runMobileSkill(cmd, mobileSkillName(platform), payload, target.timing)
		},
	}

	addMobileTargetFlags(cmd, platform, &target)
	addMobileTimingFlags(cmd, &target.timing)
	addMobileSwipeFlags(cmd.Flags(), &swipe)
	return cmd
}

func newMobileGestureCommand(platform mobilePlatform) *cobra.Command {
	var target mobileTargetFlags
	var duration float64

	cmd := &cobra.Command{
		Use:   "gesture <preset>",
		Short: "Run a gesture preset",
		Example: `  foxctl mobile ios gesture scroll-up --udid "$UDID"
  foxctl mobile ios gesture swipe-from-left-edge --screen-width 430 --screen-height 932 --udid "$UDID"`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			coords, err := mobileGestureCoordinates(args[0], target.screenWidth, target.screenHeight)
			if err != nil {
				return err
			}
			payload := mobileBasePayload(platform, target, "swipe")
			payload["x"] = coords.startX
			payload["y"] = coords.startY
			payload["x2"] = coords.endX
			payload["y2"] = coords.endY
			if platform == mobileAndroid {
				payload["duration"] = mobileDurationMS(duration)
			}
			return runMobileSkill(cmd, mobileSkillName(platform), payload, target.timing)
		},
	}

	addMobileTargetFlags(cmd, platform, &target)
	addMobileTimingFlags(cmd, &target.timing)
	cmd.Flags().Float64Var(&duration, "duration", 0.3, "Gesture duration in seconds")
	return cmd
}

func newMobileTypeCommand(platform mobilePlatform) *cobra.Command {
	var target mobileTargetFlags
	var fromStdin bool
	var file string

	cmd := &cobra.Command{
		Use:   "type [text]",
		Short: "Type text into the focused element",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			text, err := mobileReadTextInput(cmd.InOrStdin(), args, fromStdin, file)
			if err != nil {
				return err
			}
			payload := mobileBasePayload(platform, target, "type_text")
			payload["text"] = text
			return runMobileSkill(cmd, mobileSkillName(platform), payload, target.timing)
		},
	}

	addMobileTargetFlags(cmd, platform, &target)
	addMobileTimingFlags(cmd, &target.timing)
	cmd.Flags().BoolVar(&fromStdin, "stdin", false, "Read text from stdin")
	cmd.Flags().StringVar(&file, "file", "", "Read text from file")
	return cmd
}

func newMobileButtonCommand(platform mobilePlatform) *cobra.Command {
	var target mobileTargetFlags
	var duration float64

	cmd := &cobra.Command{
		Use:   "button <name>",
		Short: "Press a hardware or system button",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			payload := mobileBasePayload(platform, target, mobileButtonOperation(platform))
			if platform == mobileIOS {
				payload["button"] = mobileIOSButtonName(args[0])
			} else {
				payload["keycode"] = mobileAndroidKeycode(args[0])
			}
			if duration > 0 && platform == mobileAndroid {
				payload["duration"] = mobileDurationMS(duration)
			}
			return runMobileSkill(cmd, mobileSkillName(platform), payload, target.timing)
		},
	}

	addMobileTargetFlags(cmd, platform, &target)
	addMobileTimingFlags(cmd, &target.timing)
	cmd.Flags().Float64Var(&duration, "duration", 0, "Hold duration in seconds when supported")
	return cmd
}

func newMobileLaunchCommand(platform mobilePlatform) *cobra.Command {
	var target mobileTargetFlags

	cmd := &cobra.Command{
		Use:   "launch <bundle-id|package>",
		Short: "Launch an installed app",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			payload := mobileBasePayload(platform, target, "launch")
			payload["app"] = args[0]
			return runMobileSkill(cmd, mobileSkillName(platform), payload, target.timing)
		},
	}

	addMobileTargetFlags(cmd, platform, &target)
	addMobileTimingFlags(cmd, &target.timing)
	return cmd
}

func newMobileTerminateCommand(platform mobilePlatform) *cobra.Command {
	var target mobileTargetFlags

	cmd := &cobra.Command{
		Use:   "terminate <bundle-id|package>",
		Short: "Terminate a running app",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			payload := mobileBasePayload(platform, target, "terminate")
			payload["app"] = args[0]
			return runMobileSkill(cmd, mobileSkillName(platform), payload, target.timing)
		},
	}

	addMobileTargetFlags(cmd, platform, &target)
	addMobileTimingFlags(cmd, &target.timing)
	return cmd
}

func newMobileOpenURLCommand(platform mobilePlatform) *cobra.Command {
	var target mobileTargetFlags

	cmd := &cobra.Command{
		Use:   "open-url <url>",
		Short: "Open a URL or deep link",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			payload := mobileBasePayload(platform, target, "open_url")
			payload["url"] = args[0]
			return runMobileSkill(cmd, mobileSkillName(platform), payload, target.timing)
		},
	}

	addMobileTargetFlags(cmd, platform, &target)
	addMobileTimingFlags(cmd, &target.timing)
	return cmd
}

func newMobileKeyCommand(platform mobilePlatform) *cobra.Command {
	var target mobileTargetFlags
	var duration float64

	cmd := &cobra.Command{
		Use:   "key <keycode>",
		Short: "Press an Android keycode",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			payload := mobileBasePayload(platform, target, "press_key")
			payload["keycode"] = mobileAndroidKeycode(args[0])
			if duration > 0 {
				payload["duration"] = mobileDurationMS(duration)
			}
			return runMobileSkill(cmd, mobileSkillName(platform), payload, target.timing)
		},
	}

	addMobileTargetFlags(cmd, platform, &target)
	addMobileTimingFlags(cmd, &target.timing)
	cmd.Flags().Float64Var(&duration, "duration", 0, "Hold duration in seconds when supported")
	return cmd
}

func newMobileScreenshotCommand(platform mobilePlatform) *cobra.Command {
	var target mobileTargetFlags
	var output string

	cmd := &cobra.Command{
		Use:   "screenshot",
		Short: "Capture a screenshot",
		RunE: func(cmd *cobra.Command, _ []string) error {
			payload := mobileBasePayload(platform, target, "screenshot")
			path, err := mobileScreenshotPath(platform, output)
			if err != nil {
				return err
			}
			payload["output"] = path
			return runMobileSkill(cmd, mobileSkillName(platform), payload, target.timing)
		},
	}

	addMobileTargetFlags(cmd, platform, &target)
	addMobileTimingFlags(cmd, &target.timing)
	cmd.Flags().StringVarP(&output, "output", "o", "", "Output file or directory")
	return cmd
}

func newMobileDescribeUICommand(platform mobilePlatform) *cobra.Command {
	var target mobileTargetFlags
	var point string

	cmd := &cobra.Command{
		Use:     "describe-ui",
		Aliases: []string{"ui-tree"},
		Short:   "Describe the accessibility tree",
		RunE: func(cmd *cobra.Command, _ []string) error {
			payload := mobileBasePayload(platform, target, "ui_tree")
			if point != "" {
				if platform != mobileIOS {
					return fmt.Errorf("--point is currently only supported for iOS describe-ui")
				}
				x, y, err := parseMobilePoint(point)
				if err != nil {
					return err
				}
				payload = mobileBasePayload(platform, target, "describe_point")
				payload["x"] = x
				payload["y"] = y
			}
			return runMobileSkill(cmd, mobileSkillName(platform), payload, target.timing)
		},
	}

	addMobileTargetFlags(cmd, platform, &target)
	addMobileTimingFlags(cmd, &target.timing)
	cmd.Flags().StringVar(&point, "point", "", "Describe element at point x,y")
	return cmd
}

func newMobileBatchCommand(platform mobilePlatform) *cobra.Command {
	var target mobileTargetFlags
	var steps []string
	var file string
	var fromStdin bool
	var continueOnError bool
	var axCache string
	var typeSubmission string

	cmd := &cobra.Command{
		Use:   "batch",
		Short: "Run multiple mobile interaction steps",
		RunE: func(cmd *cobra.Command, _ []string) error {
			lines, source, err := mobileBatchLines(cmd.InOrStdin(), steps, file, fromStdin)
			if err != nil {
				return err
			}
			state := &mobileBatchState{}
			if axCache == "perBatch" {
				state.axCache = make(map[mobilePlatform][]any)
			} else if axCache != "none" {
				return fmt.Errorf("unknown --ax-cache %q (valid: perBatch, none)", axCache)
			}
			if typeSubmission != "chunked" && typeSubmission != "single" {
				return fmt.Errorf("unknown --type-submission %q (valid: chunked, single)", typeSubmission)
			}

			results := make([]map[string]any, 0, len(lines))
			for idx, line := range lines {
				trimmed := strings.TrimSpace(line)
				if trimmed == "" {
					continue
				}
				stepResult := map[string]any{
					"index": idx + 1,
					"step":  trimmed,
				}
				err := executeMobileBatchStep(cmd, platform, target, state, trimmed, typeSubmission)
				if err != nil {
					stepResult["status"] = "error"
					stepResult["error"] = err.Error()
					results = append(results, stepResult)
					if !continueOnError {
						return protocol.WriteOK(cmd.OutOrStdout(), "foxctl.mobile.batch", map[string]any{
							"platform":          platform,
							"source":            source,
							"success":           false,
							"failed_step":       idx + 1,
							"continue_on_error": continueOnError,
							"results":           results,
						})
					}
					continue
				}
				stepResult["status"] = "ok"
				results = append(results, stepResult)
			}

			return protocol.WriteOK(cmd.OutOrStdout(), "foxctl.mobile.batch", map[string]any{
				"platform":          platform,
				"source":            source,
				"success":           true,
				"continue_on_error": continueOnError,
				"results":           results,
			})
		},
	}

	addMobileTargetFlags(cmd, platform, &target)
	cmd.Flags().StringArrayVar(&steps, "step", nil, "Batch step; may be repeated")
	cmd.Flags().StringVar(&file, "file", "", "Read batch steps from file")
	cmd.Flags().BoolVar(&fromStdin, "stdin", false, "Read batch steps from stdin")
	cmd.Flags().BoolVar(&continueOnError, "continue-on-error", false, "Continue after a step fails")
	cmd.Flags().StringVar(&axCache, "ax-cache", "perBatch", "Accessibility cache mode: perBatch or none")
	cmd.Flags().StringVar(&typeSubmission, "type-submission", "chunked", "Text typing mode: chunked or single")
	return cmd
}

func addMobileTargetFlags(cmd *cobra.Command, platform mobilePlatform, target *mobileTargetFlags) {
	if platform == mobileIOS {
		cmd.Flags().StringVar(&target.udid, "udid", "", "Simulator UDID")
		target.screenWidth = 430
		target.screenHeight = 932
	} else {
		cmd.Flags().StringVar(&target.serial, "serial", "", "Android emulator/device serial")
		cmd.Flags().StringVar(&target.serial, "udid", "", "Alias for --serial")
		target.screenWidth = 1080
		target.screenHeight = 1920
	}
	cmd.Flags().IntVar(&target.screenWidth, "screen-width", target.screenWidth, "Screen width for gesture presets")
	cmd.Flags().IntVar(&target.screenHeight, "screen-height", target.screenHeight, "Screen height for gesture presets")
}

func addMobileTimingFlags(cmd *cobra.Command, timing *mobileTimingFlags) {
	cmd.Flags().Float64Var(&timing.preDelay, "pre-delay", 0, "Delay before the action in seconds")
	cmd.Flags().Float64Var(&timing.postDelay, "post-delay", 0, "Delay after the action in seconds")
}

func addMobileSwipeFlags(flags *pflag.FlagSet, swipe *mobileSwipeFlags) {
	flags.IntVar(&swipe.startX, "start-x", 0, "Start X coordinate")
	flags.IntVar(&swipe.startY, "start-y", 0, "Start Y coordinate")
	flags.IntVar(&swipe.endX, "end-x", 0, "End X coordinate")
	flags.IntVar(&swipe.endY, "end-y", 0, "End Y coordinate")
	flags.Float64Var(&swipe.duration, "duration", 0.3, "Swipe duration in seconds")
}

func mobileSkillName(platform mobilePlatform) string {
	if platform == mobileIOS {
		return "mobile/ios"
	}
	return "mobile/android"
}

func mobileBasePayload(platform mobilePlatform, target mobileTargetFlags, operation string) map[string]any {
	payload := map[string]any{"operation": operation}
	if platform == mobileIOS {
		if target.udid != "" {
			payload["udid"] = target.udid
		}
	} else if target.serial != "" {
		payload["serial"] = target.serial
	}
	return payload
}

func mobileButtonOperation(platform mobilePlatform) string {
	if platform == mobileIOS {
		return "button"
	}
	return "press_key"
}

func mobileTapPayload(cmd *cobra.Command, platform mobilePlatform, target mobileTargetFlags, tap mobileTapFlags, state *mobileBatchState) (map[string]any, error) {
	hasX := cmd.Flags().Changed("x")
	hasY := cmd.Flags().Changed("y")
	hasSelector := tap.id != "" || tap.label != ""
	if hasSelector && (hasX || hasY) {
		return nil, fmt.Errorf("use either coordinates or a selector, not both")
	}

	payload := mobileBasePayload(platform, target, "tap")
	if hasSelector {
		x, y, err := resolveMobileSelector(cmd, platform, target, tap.id, tap.label, state)
		if err != nil {
			return nil, err
		}
		payload["x"] = x
		payload["y"] = y
		return payload, nil
	}
	if !hasX || !hasY {
		return nil, fmt.Errorf("tap requires -x and -y, or --id/--label")
	}
	payload["x"] = tap.x
	payload["y"] = tap.y
	return payload, nil
}

func mobileSwipePayload(cmd *cobra.Command, platform mobilePlatform, target mobileTargetFlags, swipe mobileSwipeFlags) (map[string]any, error) {
	for _, name := range []string{"start-x", "start-y", "end-x", "end-y"} {
		if !cmd.Flags().Changed(name) {
			return nil, fmt.Errorf("swipe requires --%s", name)
		}
	}
	payload := mobileBasePayload(platform, target, "swipe")
	payload["x"] = swipe.startX
	payload["y"] = swipe.startY
	payload["x2"] = swipe.endX
	payload["y2"] = swipe.endY
	if platform == mobileAndroid {
		payload["duration"] = mobileDurationMS(swipe.duration)
	}
	return payload, nil
}

func runMobileSkill(cmd *cobra.Command, skillName string, payload map[string]any, timing mobileTimingFlags) error {
	stdout, stderr, err := runMobileSkillBytes(cmd, skillName, payload, timing)
	if len(stderr) > 0 {
		if _, werr := cmd.ErrOrStderr().Write(append(stderr, '\n')); werr != nil {
			return werr
		}
	}
	if err != nil {
		return err
	}
	return memorycmd.WriteEnvelope(cmd.OutOrStdout(), stdout)
}

func runMobileSkillEnvelope(cmd *cobra.Command, skillName string, payload map[string]any, timing mobileTimingFlags) (mobileEnvelope, error) {
	stdout, stderr, err := runMobileSkillBytes(cmd, skillName, payload, timing)
	if len(stderr) > 0 {
		if _, werr := cmd.ErrOrStderr().Write(append(stderr, '\n')); werr != nil {
			return mobileEnvelope{}, werr
		}
	}
	if err != nil {
		return mobileEnvelope{}, err
	}
	var env mobileEnvelope
	if err := json.Unmarshal(stdout, &env); err != nil {
		return mobileEnvelope{}, fmt.Errorf("decode mobile skill envelope: %w", err)
	}
	if env.Status == "error" {
		if msg, ok := env.Error["message"].(string); ok && msg != "" {
			return env, fmt.Errorf("%s", msg)
		}
		return env, fmt.Errorf("mobile skill returned error")
	}
	return env, nil
}

func runMobileSkillBytes(cmd *cobra.Command, skillName string, payload map[string]any, timing mobileTimingFlags) ([]byte, []byte, error) {
	if err := sleepSeconds(timing.preDelay); err != nil {
		return nil, nil, err
	}
	cfg := config.MustFromContext(cmd.Context())
	handle, err := findSkill(cfg, skillName)
	if err != nil {
		return nil, nil, err
	}
	input, err := json.Marshal(payload)
	if err != nil {
		return nil, nil, err
	}
	stdout, stderr, err := executeSkill(cmd.Context(), handle.Manifest, handle.ArtifactPath, input)
	if err != nil {
		return stdout, stderr, err
	}
	if err := sleepSeconds(timing.postDelay); err != nil {
		return stdout, stderr, err
	}
	return stdout, stderr, nil
}

func sleepSeconds(seconds float64) error {
	if seconds < 0 {
		return fmt.Errorf("delay must be non-negative")
	}
	if seconds == 0 {
		return nil
	}
	time.Sleep(time.Duration(seconds * float64(time.Second)))
	return nil
}

type mobileGesture struct {
	startX int
	startY int
	endX   int
	endY   int
}

func mobileGestureCoordinates(name string, width, height int) (mobileGesture, error) {
	if width <= 0 || height <= 0 {
		return mobileGesture{}, fmt.Errorf("screen dimensions must be positive")
	}
	x := func(r float64) int { return int(math.Round(float64(width) * r)) }
	y := func(r float64) int { return int(math.Round(float64(height) * r)) }

	switch name {
	case "scroll-up":
		return mobileGesture{startX: x(0.5), startY: y(0.72), endX: x(0.5), endY: y(0.28)}, nil
	case "scroll-down":
		return mobileGesture{startX: x(0.5), startY: y(0.28), endX: x(0.5), endY: y(0.72)}, nil
	case "scroll-left":
		return mobileGesture{startX: x(0.78), startY: y(0.5), endX: x(0.22), endY: y(0.5)}, nil
	case "scroll-right":
		return mobileGesture{startX: x(0.22), startY: y(0.5), endX: x(0.78), endY: y(0.5)}, nil
	case "swipe-from-left-edge":
		return mobileGesture{startX: x(0.02), startY: y(0.5), endX: x(0.55), endY: y(0.5)}, nil
	case "swipe-from-right-edge":
		return mobileGesture{startX: x(0.98), startY: y(0.5), endX: x(0.45), endY: y(0.5)}, nil
	case "swipe-from-top-edge":
		return mobileGesture{startX: x(0.5), startY: y(0.02), endX: x(0.5), endY: y(0.55)}, nil
	case "swipe-from-bottom-edge":
		return mobileGesture{startX: x(0.5), startY: y(0.98), endX: x(0.5), endY: y(0.45)}, nil
	default:
		return mobileGesture{}, fmt.Errorf("unknown gesture preset %q", name)
	}
}

func mobileDurationMS(seconds float64) int {
	if seconds <= 0 {
		return 300
	}
	return int(math.Round(seconds * 1000))
}

func mobileScreenshotPath(platform mobilePlatform, output string) (string, error) {
	if output == "" {
		return fmt.Sprintf("%s_screenshot_%s.png", platform, time.Now().UTC().Format("20060102_150405")), nil
	}
	info, err := os.Stat(output)
	if err == nil && info.IsDir() {
		return filepath.Join(output, fmt.Sprintf("%s_screenshot_%s.png", platform, time.Now().UTC().Format("20060102_150405"))), nil
	}
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	if strings.HasSuffix(output, string(os.PathSeparator)) {
		return "", fmt.Errorf("screenshot output directory does not exist: %s", output)
	}
	return output, nil
}

func mobileReadTextInput(stdin io.Reader, args []string, fromStdin bool, file string) (string, error) {
	sources := 0
	if len(args) > 0 {
		sources++
	}
	if fromStdin {
		sources++
	}
	if file != "" {
		sources++
	}
	if sources != 1 {
		return "", fmt.Errorf("provide exactly one text source: args, --stdin, or --file")
	}
	if fromStdin {
		data, err := io.ReadAll(stdin)
		if err != nil {
			return "", err
		}
		return string(data), nil
	}
	if file != "" {
		data, err := os.ReadFile(file)
		if err != nil {
			return "", err
		}
		return string(data), nil
	}
	return strings.Join(args, " "), nil
}

func mobileBatchLines(stdin io.Reader, steps []string, file string, fromStdin bool) ([]string, string, error) {
	sources := 0
	source := ""
	if len(steps) > 0 {
		sources++
		source = "step"
	}
	if file != "" {
		sources++
		source = "file"
	}
	if fromStdin {
		sources++
		source = "stdin"
	}
	if sources != 1 {
		return nil, "", fmt.Errorf("provide exactly one step source: --step, --file, or --stdin")
	}
	if len(steps) > 0 {
		return steps, source, nil
	}
	var data []byte
	var err error
	if file != "" {
		data, err = os.ReadFile(file)
	} else {
		data, err = io.ReadAll(stdin)
	}
	if err != nil {
		return nil, "", err
	}
	return strings.Split(string(data), "\n"), source, nil
}

func executeMobileBatchStep(cmd *cobra.Command, platform mobilePlatform, target mobileTargetFlags, state *mobileBatchState, line, typeSubmission string) error {
	argv, err := shellreduce.SplitCommand(line)
	if err != nil {
		return err
	}
	if len(argv) == 0 {
		return nil
	}
	switch argv[0] {
	case "sleep":
		if len(argv) != 2 {
			return fmt.Errorf("sleep requires one duration argument")
		}
		seconds, err := strconv.ParseFloat(argv[1], 64)
		if err != nil {
			return fmt.Errorf("parse sleep duration: %w", err)
		}
		return sleepSeconds(seconds)
	case "tap":
		payload, err := mobileBatchTapPayload(cmd, platform, target, state, argv[1:])
		if err != nil {
			return err
		}
		_, err = runMobileSkillEnvelope(cmd, mobileSkillName(platform), payload, mobileTimingFlags{})
		return err
	case "swipe":
		payload, err := mobileBatchSwipePayload(platform, target, argv[1:])
		if err != nil {
			return err
		}
		_, err = runMobileSkillEnvelope(cmd, mobileSkillName(platform), payload, mobileTimingFlags{})
		return err
	case "gesture":
		if len(argv) != 2 {
			return fmt.Errorf("gesture requires one preset")
		}
		coords, err := mobileGestureCoordinates(argv[1], target.screenWidth, target.screenHeight)
		if err != nil {
			return err
		}
		payload := mobileBasePayload(platform, target, "swipe")
		payload["x"] = coords.startX
		payload["y"] = coords.startY
		payload["x2"] = coords.endX
		payload["y2"] = coords.endY
		if platform == mobileAndroid {
			payload["duration"] = 300
		}
		_, err = runMobileSkillEnvelope(cmd, mobileSkillName(platform), payload, mobileTimingFlags{})
		return err
	case "type":
		if len(argv) < 2 {
			return fmt.Errorf("type requires text")
		}
		text := strings.Join(argv[1:], " ")
		if typeSubmission == "chunked" {
			return executeMobileChunkedType(cmd, platform, target, text)
		}
		payload := mobileBasePayload(platform, target, "type_text")
		payload["text"] = text
		_, err = runMobileSkillEnvelope(cmd, mobileSkillName(platform), payload, mobileTimingFlags{})
		return err
	case "button":
		if len(argv) != 2 {
			return fmt.Errorf("button requires one name")
		}
		payload := mobileBasePayload(platform, target, mobileButtonOperation(platform))
		if platform == mobileIOS {
			payload["button"] = mobileIOSButtonName(argv[1])
		} else {
			payload["keycode"] = mobileAndroidKeycode(argv[1])
		}
		_, err = runMobileSkillEnvelope(cmd, mobileSkillName(platform), payload, mobileTimingFlags{})
		return err
	case "key":
		if platform != mobileAndroid {
			return fmt.Errorf("key is only supported for Android")
		}
		if len(argv) != 2 {
			return fmt.Errorf("key requires one keycode")
		}
		payload := mobileBasePayload(platform, target, "press_key")
		payload["keycode"] = mobileAndroidKeycode(argv[1])
		_, err = runMobileSkillEnvelope(cmd, mobileSkillName(platform), payload, mobileTimingFlags{})
		return err
	case "screenshot":
		payload := mobileBasePayload(platform, target, "screenshot")
		fs := newMobileBatchFlagSet("screenshot")
		var output string
		fs.StringVarP(&output, "output", "o", "", "Output file or directory")
		if err := fs.Parse(argv[1:]); err != nil {
			return err
		}
		path, err := mobileScreenshotPath(platform, output)
		if err != nil {
			return err
		}
		payload["output"] = path
		_, err = runMobileSkillEnvelope(cmd, mobileSkillName(platform), payload, mobileTimingFlags{})
		return err
	default:
		return fmt.Errorf("unknown batch step %q", argv[0])
	}
}

func mobileBatchTapPayload(cmd *cobra.Command, platform mobilePlatform, target mobileTargetFlags, state *mobileBatchState, argv []string) (map[string]any, error) {
	fs := newMobileBatchFlagSet("tap")
	var tap mobileTapFlags
	fs.IntVarP(&tap.x, "x", "x", 0, "X coordinate")
	fs.IntVarP(&tap.y, "y", "y", 0, "Y coordinate")
	fs.StringVar(&tap.id, "id", "", "Accessibility identifier or resource ID")
	fs.StringVar(&tap.label, "label", "", "Accessibility label or text")
	if err := fs.Parse(argv); err != nil {
		return nil, err
	}
	hasX := fs.Changed("x")
	hasY := fs.Changed("y")
	hasSelector := tap.id != "" || tap.label != ""
	if hasSelector && (hasX || hasY) {
		return nil, fmt.Errorf("use either coordinates or a selector, not both")
	}
	payload := mobileBasePayload(platform, target, "tap")
	if hasSelector {
		x, y, err := resolveMobileSelector(cmd, platform, target, tap.id, tap.label, state)
		if err != nil {
			return nil, err
		}
		payload["x"] = x
		payload["y"] = y
		return payload, nil
	}
	if !hasX || !hasY {
		return nil, fmt.Errorf("tap requires -x and -y, or --id/--label")
	}
	payload["x"] = tap.x
	payload["y"] = tap.y
	return payload, nil
}

func mobileBatchSwipePayload(platform mobilePlatform, target mobileTargetFlags, argv []string) (map[string]any, error) {
	fs := newMobileBatchFlagSet("swipe")
	var swipe mobileSwipeFlags
	addMobileSwipeFlags(fs, &swipe)
	if err := fs.Parse(argv); err != nil {
		return nil, err
	}
	for _, name := range []string{"start-x", "start-y", "end-x", "end-y"} {
		if !fs.Changed(name) {
			return nil, fmt.Errorf("swipe requires --%s", name)
		}
	}
	payload := mobileBasePayload(platform, target, "swipe")
	payload["x"] = swipe.startX
	payload["y"] = swipe.startY
	payload["x2"] = swipe.endX
	payload["y2"] = swipe.endY
	if platform == mobileAndroid {
		payload["duration"] = mobileDurationMS(swipe.duration)
	}
	return payload, nil
}

func newMobileBatchFlagSet(name string) *pflag.FlagSet {
	fs := pflag.NewFlagSet(name, pflag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs
}

func executeMobileChunkedType(cmd *cobra.Command, platform mobilePlatform, target mobileTargetFlags, text string) error {
	const chunkSize = 80
	if len([]rune(text)) <= chunkSize {
		payload := mobileBasePayload(platform, target, "type_text")
		payload["text"] = text
		_, err := runMobileSkillEnvelope(cmd, mobileSkillName(platform), payload, mobileTimingFlags{})
		return err
	}
	runes := []rune(text)
	for start := 0; start < len(runes); start += chunkSize {
		end := start + chunkSize
		if end > len(runes) {
			end = len(runes)
		}
		payload := mobileBasePayload(platform, target, "type_text")
		payload["text"] = string(runes[start:end])
		if _, err := runMobileSkillEnvelope(cmd, mobileSkillName(platform), payload, mobileTimingFlags{}); err != nil {
			return err
		}
	}
	return nil
}

func resolveMobileSelector(cmd *cobra.Command, platform mobilePlatform, target mobileTargetFlags, id, label string, state *mobileBatchState) (int, int, error) {
	var elements []any
	if state != nil && state.axCache != nil {
		elements = state.axCache[platform]
	}
	if elements == nil {
		payload := mobileBasePayload(platform, target, "ui_tree")
		payload["full"] = true
		env, err := runMobileSkillEnvelope(cmd, mobileSkillName(platform), payload, mobileTimingFlags{})
		if err != nil {
			return 0, 0, err
		}
		elements, _ = env.Data["elements"].([]any)
		if state != nil && state.axCache != nil {
			state.axCache[platform] = elements
		}
	}
	x, y, ok := findMobileElementCenter(platform, elements, id, label)
	if !ok {
		return 0, 0, fmt.Errorf("no accessibility element matched selector")
	}
	return x, y, nil
}

func findMobileElementCenter(platform mobilePlatform, elements []any, id, label string) (int, int, bool) {
	for _, elem := range elements {
		m, ok := elem.(map[string]any)
		if !ok {
			continue
		}
		if id != "" && !mobileFieldEqualsAny(m, id, mobileIDFields(platform)) {
			continue
		}
		if label != "" && !mobileFieldEqualsAny(m, label, mobileLabelFields(platform)) {
			continue
		}
		if x, y, ok := mobileElementCenter(m); ok {
			return x, y, true
		}
	}
	return 0, 0, false
}

func mobileIDFields(platform mobilePlatform) []string {
	if platform == mobileAndroid {
		return []string{"resource_id", "resource-id", "id", "identifier"}
	}
	return []string{"AXUniqueId", "identifier", "id", "accessibility_id", "name"}
}

func mobileLabelFields(platform mobilePlatform) []string {
	if platform == mobileAndroid {
		return []string{"text", "content_desc", "content-desc", "label"}
	}
	return []string{"AXLabel", "AXValue", "label", "title", "name", "value", "text"}
}

func mobileFieldEqualsAny(m map[string]any, want string, fields []string) bool {
	for _, field := range fields {
		if got, ok := m[field].(string); ok && got == want {
			return true
		}
	}
	return false
}

func mobileElementCenter(m map[string]any) (int, int, bool) {
	for _, key := range []string{"bounds", "frame", "rect"} {
		if raw, ok := m[key]; ok {
			if x, y, ok := mobileCenterFromValue(raw); ok {
				return x, y, true
			}
		}
	}
	if x, okX := numberFromAny(m["x"]); okX {
		if y, okY := numberFromAny(m["y"]); okY {
			if w, okW := numberFromAny(firstPresent(m, "width", "w")); okW {
				if h, okH := numberFromAny(firstPresent(m, "height", "h")); okH {
					return int(math.Round(x + w/2)), int(math.Round(y + h/2)), true
				}
			}
			return int(math.Round(x)), int(math.Round(y)), true
		}
	}
	return 0, 0, false
}

func mobileCenterFromValue(raw any) (int, int, bool) {
	switch v := raw.(type) {
	case string:
		return mobileCenterFromBoundsString(v)
	case map[string]any:
		return mobileCenterFromMap(v)
	default:
		return 0, 0, false
	}
}

func mobileCenterFromMap(m map[string]any) (int, int, bool) {
	if x, okX := numberFromAny(firstPresent(m, "x", "X", "left", "minX")); okX {
		if y, okY := numberFromAny(firstPresent(m, "y", "Y", "top", "minY")); okY {
			if w, okW := numberFromAny(firstPresent(m, "width", "Width", "w")); okW {
				if h, okH := numberFromAny(firstPresent(m, "height", "Height", "h")); okH {
					return int(math.Round(x + w/2)), int(math.Round(y + h/2)), true
				}
			}
			if right, okR := numberFromAny(firstPresent(m, "right", "maxX")); okR {
				if bottom, okB := numberFromAny(firstPresent(m, "bottom", "maxY")); okB {
					return int(math.Round((x + right) / 2)), int(math.Round((y + bottom) / 2)), true
				}
			}
		}
	}
	return 0, 0, false
}

var (
	androidBoundsRE = regexp.MustCompile(`^\[(\d+),(\d+)\]\[(\d+),(\d+)\]$`)
	numberRE        = regexp.MustCompile(`-?\d+(?:\.\d+)?`)
)

func mobileCenterFromBoundsString(bounds string) (int, int, bool) {
	if matches := androidBoundsRE.FindStringSubmatch(bounds); matches != nil {
		x1, _ := strconv.Atoi(matches[1])
		y1, _ := strconv.Atoi(matches[2])
		x2, _ := strconv.Atoi(matches[3])
		y2, _ := strconv.Atoi(matches[4])
		return (x1 + x2) / 2, (y1 + y2) / 2, true
	}
	nums := numberRE.FindAllString(bounds, -1)
	if len(nums) < 4 {
		return 0, 0, false
	}
	values := make([]float64, 4)
	for i := 0; i < 4; i++ {
		v, err := strconv.ParseFloat(nums[i], 64)
		if err != nil {
			return 0, 0, false
		}
		values[i] = v
	}
	x := values[0] + values[2]/2
	y := values[1] + values[3]/2
	return int(math.Round(x)), int(math.Round(y)), true
}

func firstPresent(m map[string]any, keys ...string) any {
	for _, key := range keys {
		if v, ok := m[key]; ok {
			return v
		}
	}
	return nil
}

func numberFromAny(v any) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case float64:
		return n, true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(n, 64)
		return f, err == nil
	default:
		return 0, false
	}
}

func parseMobilePoint(point string) (int, int, error) {
	parts := strings.Split(point, ",")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("point must be x,y")
	}
	x, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return 0, 0, fmt.Errorf("parse point x: %w", err)
	}
	y, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil {
		return 0, 0, fmt.Errorf("parse point y: %w", err)
	}
	return x, y, nil
}

func mobileIOSButtonName(name string) string {
	switch strings.ToLower(strings.ReplaceAll(name, "-", "_")) {
	case "home":
		return "HOME"
	case "lock":
		return "LOCK"
	case "side_button":
		return "SIDE_BUTTON"
	case "siri":
		return "SIRI"
	case "apple_pay":
		return "APPLE_PAY"
	default:
		return strings.ToUpper(name)
	}
}

func mobileAndroidKeycode(name string) string {
	upper := strings.ToUpper(strings.ReplaceAll(name, "-", "_"))
	if strings.HasPrefix(upper, "KEYCODE_") {
		return upper
	}
	return "KEYCODE_" + upper
}
