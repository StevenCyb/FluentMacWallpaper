// FluentMacWallpaper — menu bar app that loops a video as the desktop wallpaper.
package main

import (
	"bytes"
	_ "embed"
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"github.com/progrium/darwinkit/macos"
	"github.com/progrium/darwinkit/macos/appkit"
	"github.com/progrium/darwinkit/macos/avfoundation"
	"github.com/progrium/darwinkit/macos/foundation"
	"github.com/progrium/darwinkit/macos/uti"
	"github.com/progrium/darwinkit/objc"
)

// kCGDesktopWindowLevel: below desktop icons, so icons and clicks stay normal.
const desktopWindowLevel appkit.WindowLevel = -2147483623

// no signed .app bundle to register with SMAppService, so autostart via a plain LaunchAgent instead.
const launchAgentLabel = "com.stevencyb.fluentmacwallpaper"

//go:embed plist.xml
var launchAgentPlist string

// menu bar is 22pt; 18pt is the standard status item icon size
const statusIconSize = 18.0

var (
	windows       []appkit.Window
	windowVisible []bool // parallel to windows; occluded windows (e.g. under a fullscreen app) don't count
	player        avfoundation.QueuePlayer
	looper        avfoundation.PlayerLooper
	playing       bool // user's play/pause intent, via the menu item

	// nobody can see the wallpaper right now; decoding/rendering would be wasted
	locked         bool
	displaysAsleep bool
)

// derived from icon.png: black glyph + alpha; macOS tints template images
// to match light/dark menu bars automatically
//
//go:embed icon.png
var iconPNG []byte

func main() {
	// DYLD_LIBRARY_PATH (e.g. Homebrew lib dirs from a shell profile) makes system
	// frameworks like ImageIO load the wrong dylibs → SIGBUS at 0xbad4007.
	// Re-exec ourselves with a clean dyld environment.
	if os.Getenv("DYLD_LIBRARY_PATH") != "" {
		_ = os.Unsetenv("DYLD_LIBRARY_PATH")

		if exe, err := os.Executable(); err == nil {
			// returns only on error; fall through and run with the unset env
			_ = syscall.Exec(exe, os.Args, os.Environ()) //nolint:gosec // re-exec of our own binary
		}
	}

	macos.RunApp(launch)
}

func launch(app appkit.Application, delegate *appkit.ApplicationDelegate) {
	app.SetActivationPolicy(appkit.ApplicationActivationPolicyAccessory) // no dock icon

	// screens added/removed/rearranged (e.g. external monitor plugged in)
	delegate.SetApplicationDidChangeScreenParameters(func(foundation.Notification) {
		rebuildWindows()
	})

	// pause decode/render while nothing is visible: displays asleep or screen locked
	workspaceNC := appkit.Workspace_SharedWorkspace().NotificationCenter()
	workspaceNC.AddObserverForNameObjectQueueUsingBlock("NSWorkspaceScreensDidSleepNotification", nil, foundation.OperationQueue_MainQueue(), func(foundation.Notification) {
		displaysAsleep = true
		applyPlaybackState()
	})
	workspaceNC.AddObserverForNameObjectQueueUsingBlock("NSWorkspaceScreensDidWakeNotification", nil, foundation.OperationQueue_MainQueue(), func(foundation.Notification) {
		displaysAsleep = false
		applyPlaybackState()
	})

	distributedNC := foundation.DistributedNotificationCenter_NotificationCenterForType(foundation.LocalNotificationCenterType)
	distributedNC.AddObserverForNameObjectQueueUsingBlock("com.apple.screenIsLocked", nil, foundation.OperationQueue_MainQueue(), func(foundation.Notification) {
		locked = true
		applyPlaybackState()
	})
	distributedNC.AddObserverForNameObjectQueueUsingBlock("com.apple.screenIsUnlocked", nil, foundation.OperationQueue_MainQueue(), func(foundation.Notification) {
		locked = false
		applyPlaybackState()
	})

	item := appkit.StatusBar_SystemStatusBar().StatusItemWithLength(appkit.VariableStatusItemLength)
	objc.Retain(&item)

	icon := appkit.NewImageWithDataIgnoringOrientation(iconPNG)
	icon.SetSize(foundation.Size{Width: statusIconSize, Height: statusIconSize})
	icon.SetTemplate(true)
	item.Button().SetImage(icon)

	var pauseItem appkit.MenuItem

	pauseItem = appkit.NewMenuItemWithAction("Pause", "p", func(objc.Object) {
		togglePause(pauseItem)
	})
	pauseItem.SetImage(appkit.Image_ImageWithSystemSymbolNameAccessibilityDescription("pause.fill", "Pause"))

	selectItem := appkit.NewMenuItemWithAction("Select Video…", "o", func(objc.Object) {
		selectVideo(app)
		pauseItem.SetTitle("Pause")
		pauseItem.SetImage(appkit.Image_ImageWithSystemSymbolNameAccessibilityDescription("pause.fill", "Pause"))
	})
	selectItem.SetImage(appkit.Image_ImageWithSystemSymbolNameAccessibilityDescription("video.badge.plus", "Select Video"))

	var launchAtLoginItem appkit.MenuItem

	launchAtLoginItem = appkit.NewMenuItemWithAction("Launch at Login", "", func(objc.Object) {
		toggleLaunchAtLogin(launchAtLoginItem)
	})
	launchAtLoginItem.SetImage(appkit.Image_ImageWithSystemSymbolNameAccessibilityDescription("power", "Launch at Login"))
	if launchAtLoginEnabled() {
		launchAtLoginItem.SetState(appkit.ControlStateValueOn)
	}

	menu := appkit.NewMenuWithTitle("FluentMacWallpaper")
	menu.AddItem(selectItem)
	menu.AddItem(pauseItem)
	menu.AddItem(launchAtLoginItem)
	menu.AddItem(appkit.MenuItem_SeparatorItem())
	menu.AddItem(appkit.NewMenuItemWithSelector("Quit", "q", objc.Sel("terminate:")))
	item.SetMenu(menu)

	if len(os.Args) > 1 {
		setVideo(foundation.URL_FileURLWithPath(os.Args[1]))
	}
}

func selectVideo(app appkit.Application) {
	panel := appkit.OpenPanel_OpenPanel()
	panel.SetAllowsMultipleSelection(false)
	panel.SetCanChooseDirectories(false)
	panel.SetAllowedContentTypes([]uti.IType{
		uti.Type_TypeWithFilenameExtension("mp4"),
		uti.Type_TypeWithFilenameExtension("mov"),
		uti.Type_TypeWithFilenameExtension("m4v"),
	})
	app.ActivateIgnoringOtherApps(true)

	if panel.RunModal() != appkit.ModalResponseOK {
		return
	}

	setVideo(panel.URL())
}

func setVideo(url foundation.URL) {
	if len(windows) == 0 {
		windows = makeWindows()
	}

	if !player.IsNil() {
		player.Pause()
	}

	playerItem := avfoundation.PlayerItem_PlayerItemWithURL(url)
	player = avfoundation.NewQueuePlayer()
	objc.Retain(&player)
	player.SetMuted(true)
	// AVPlayerLooper = gapless loop; AVPlayer uses VideoToolbox hardware decode.
	looper = avfoundation.PlayerLooper_PlayerLooperWithPlayerTemplateItem(player, playerItem)
	objc.Retain(&looper)

	attachLayers()
	playing = true
	applyPlaybackState()
}

// actual player state = user wants it playing AND the wallpaper is actually visible
func applyPlaybackState() {
	if player.IsNil() {
		return
	}

	if playing && !locked && !displaysAsleep && anyWindowVisible() {
		player.Play()
	} else {
		player.Pause()
	}
}

func anyWindowVisible() bool {
	for _, v := range windowVisible {
		if v {
			return true
		}
	}

	return len(windowVisible) == 0
}

// one borderless desktop window per connected screen
func makeWindows() []appkit.Window {
	var ws []appkit.Window
	windowVisible = nil

	for _, screen := range appkit.Screen_Screens() {
		w := appkit.NewWindowWithContentRectStyleMaskBackingDeferScreen(
			screen.Frame(), appkit.WindowStyleMaskBorderless, appkit.BackingStoreBuffered, false,
			screen)
		objc.Retain(&w)
		w.SetLevel(desktopWindowLevel)
		w.SetIgnoresMouseEvents(true)
		w.SetReleasedWhenClosed(false)
		w.SetCollectionBehavior(appkit.WindowCollectionBehaviorCanJoinAllSpaces |
			appkit.WindowCollectionBehaviorStationary |
			appkit.WindowCollectionBehaviorIgnoresCycle)

		idx := len(windowVisible)
		windowVisible = append(windowVisible, true)

		windowDelegate := &appkit.WindowDelegate{}
		windowDelegate.SetWindowDidChangeOcclusionState(func(foundation.Notification) {
			windowVisible[idx] = w.OcclusionState()&appkit.WindowOcclusionStateVisible != 0
			applyPlaybackState()
		})
		w.SetDelegate(windowDelegate)

		ws = append(ws, w)
	}

	return ws
}

// One AVPlayer, one decode; each window gets its own layer reading the same output.
func attachLayers() {
	for _, w := range windows {
		layer := avfoundation.PlayerLayer_PlayerLayerWithPlayer(player)
		layer.SetVideoGravity(avfoundation.LayerVideoGravityResizeAspectFill)

		view := w.ContentView()
		view.SetLayer(layer)
		view.SetWantsLayer(true)

		w.OrderFrontRegardless()
	}
}

// screen configuration changed (monitor plugged/unplugged/rearranged): drop stale
// windows and recreate for the current screen set, reusing the running player.
func rebuildWindows() {
	if player.IsNil() {
		return
	}

	for _, w := range windows {
		w.Close()
	}

	windows = makeWindows()
	attachLayers()
}

func togglePause(pauseItem appkit.MenuItem) {
	if player.IsNil() {
		return
	}

	playing = !playing
	applyPlaybackState()

	if playing {
		pauseItem.SetTitle("Pause")
		pauseItem.SetImage(appkit.Image_ImageWithSystemSymbolNameAccessibilityDescription("pause.fill", "Pause"))
	} else {
		pauseItem.SetTitle("Play")
		pauseItem.SetImage(appkit.Image_ImageWithSystemSymbolNameAccessibilityDescription("play.fill", "Play"))
	}
}

func toggleLaunchAtLogin(item appkit.MenuItem) {
	enable := item.State() != appkit.ControlStateValueOn

	if err := setLaunchAtLogin(enable); err != nil {
		return
	}

	if enable {
		item.SetState(appkit.ControlStateValueOn)
	} else {
		item.SetState(appkit.ControlStateValueOff)
	}
}

func launchAgentPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(home, "Library", "LaunchAgents", launchAgentLabel+".plist"), nil
}

func launchAtLoginEnabled() bool {
	path, err := launchAgentPath()
	if err != nil {
		return false
	}

	_, err = os.Stat(path)

	return err == nil
}

func setLaunchAtLogin(enable bool) error {
	path, err := launchAgentPath()
	if err != nil {
		return err
	}

	if !enable {
		_ = exec.Command("launchctl", "unload", path).Run()

		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}

		return nil
	}

	exe, err := os.Executable()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	var exeEscaped bytes.Buffer

	_ = xml.EscapeText(&exeEscaped, []byte(exe))

	plist := fmt.Sprintf(launchAgentPlist, launchAgentLabel, exeEscaped.String())

	return os.WriteFile(path, []byte(plist), 0o644)
}
