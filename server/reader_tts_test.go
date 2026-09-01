package server

import (
	"net/http"
	"strings"
	"testing"
)

func TestEPUBReaderTTSModesAndBackgroundControls(t *testing.T) {
	s := newAuthTestServer(t)

	read := func(path string) string {
		t.Helper()
		response := requestServer(s, http.MethodGet, path, nil)
		if response.Code != http.StatusOK {
			t.Fatalf("GET %s status=%d body=%s", path, response.Code, response.Body.String())
		}
		return response.Body.String()
	}

	index := read("/static/reader/epub/")
	for _, expected := range []string{
		`id="tts-options-panel"`,
		`id="tts-stop-after"`,
		`id="tts-duration-minutes"`,
		`id="tts-keep-screen-on"`,
		`id="tts-audio"`,
		`playsinline`,
		`id="tts-action-menu"`,
		`id="tts-action-pause"`,
		`id="tts-action-stop"`,
		`id="tts-voice"`,
		`id="tts-speed"`,
		`data-i18n="tts_action_pause"`,
		`data-i18n="tts_action_stop"`,
	} {
		if !strings.Contains(index, expected) {
			t.Errorf("EPUB TTS controls are missing %q", expected)
		}
	}
	if count := strings.Count(index, `id="tts-audio"`); count != 1 {
		t.Errorf("persistent TTS audio element count=%d, want 1", count)
	}
	for _, expected := range []string{`data-i18n="tts_options_title"`, `data-i18n="tts_start"`, `data-i18n-placeholder="search_placeholder"`, `EPUB_I18N.t('confirm_reset')`} {
		if !strings.Contains(index, expected) {
			t.Errorf("EPUB page is missing localized UI markers %q", expected)
		}
	}
	for _, expected := range []string{`id="reader-loading"`, `reader-loading-spinner`, `data-i18n="loading"`} {
		if !strings.Contains(index, expected) {
			t.Errorf("EPUB page is missing the reader loading indicator %q", expected)
		}
	}
	for _, expected := range []string{`data-mics-reader-about`, `class="setting-content mics-settings-about"`} {
		if !strings.Contains(index, expected) {
			t.Errorf("EPUB settings are missing the shared site About surface %q", expected)
		}
	}
	for _, obsolete := range []string{"ePubViewer v3.0.0", "Patrick Gaskin", "This app requires Microsoft Edge 15+"} {
		if strings.Contains(index, obsolete) {
			t.Errorf("EPUB settings still contain obsolete About copy %q", obsolete)
		}
	}
	if strings.Contains(index, "name=\"tts-mode\"") {
		t.Error("EPUB TTS must not offer a playback mode selector")
	}

	script := read("/static/reader/epub/script.js")
	for _, expected := range []string{
		`App.prototype.startTTSTimer`,
		`App.prototype.pauseTTSTimer`,
		`App.prototype.setupTTSMediaSession`,
		`navigator.mediaSession.setActionHandler`,
		`App.prototype.requestTTSWakeLock`,
		`navigator.wakeLock.request("screen")`,
		`App.prototype.onTTSVisibilityChange`,
		`App.prototype.fetchTTSBlob`,
		`App.prototype.fetchTTSTrack`,
		`App.prototype.playTTSTrack`,
		`App.prototype.refreshTTSParagraphElement`,
		`if (this.state.ttsTrackMode) {`,
		`requestAnimationFrame(() => this.updateTTSTrackParagraph(true));`,
		`if (this.isTTSParagraphVisible(paragraph)) return;`,
		`this.state.rendition.display(targetCFI).then`,
		`let visibleLeft = Math.max(0, (startPage - 1) * pageWidth);`,
		`rect.right > visibleLeft && rect.left < visibleRight`,
		`navigator.mediaSession.setPositionState`,
		`/tts/track`,
		`that.qs("#tts-audio")`,
		`setupTTSStatusDrag`,
		`pointerdown`,
		`pointermove`,
		`setPointerCapture`,
		`if (!this.state.ttsStopAfter) return;`,
		`this.state.ttsStopAfter = options.stopAfter;`,
		`this.qs("#tts-stop-after")`,
		`App.prototype.closeTTSActionMenu`,
		`App.prototype.toggleTTSActionMenu`,
		`this.state.ttsPausedMoved = true;`,
		`this.state.ttsPausedLocationCfi = null;`,
		`tts_action_pause: "Pause"`,
		`App.prototype.pickTTSVoice`,
		`en-US-GuyNeural`,
		`zh-CN-YunxiNeural`,
		`audio.playbackRate =`,
		`App.prototype.ttsSpeeds`,
		`let limit = tracks.length === 0 ? 2400 : 6000;`,
		`that.playTTSTrack(0, runId);`,
		`that.prefetchTTSTracks(runId, index + 1);`,
		`index + 3`,
	} {
		if !strings.Contains(script, expected) {
			t.Errorf("EPUB TTS implementation is missing %q", expected)
		}
	}
	if strings.Contains(script, "new Audio(") {
		t.Error("EPUB TTS must reuse its persistent audio element")
	}
	if strings.Contains(script, "tts-mode") {
		t.Error("EPUB TTS must not reference a playback mode selector")
	}
	for _, obsolete := range []string{`Promise.all(ready)`, `tracks.slice(0, Math.min(3`} {
		if strings.Contains(script, obsolete) {
			t.Errorf("EPUB TTS must not wait for multiple tracks before playback: found %q", obsolete)
		}
	}
	playStart := strings.Index(script, `App.prototype.playTTSTrack = function`)
	playEnd := strings.Index(script, `App.prototype.updateTTSTrackParagraph = function`)
	if playStart < 0 || playEnd <= playStart {
		t.Error("EPUB TTS track playback functions are missing or out of order")
	} else {
		playBody := script[playStart:playEnd]
		fetchPos := strings.Index(playBody, `this.fetchTTSTrack(track, runId, index).then`)
		prefetchPos := strings.Index(playBody, `that.prefetchTTSTracks(runId, index + 1);`)
		if fetchPos < 0 || prefetchPos <= fetchPos {
			t.Error("EPUB TTS must finish the active track request before prefetching later tracks")
		}
	}
	for _, expected := range []string{`EPUB_I18N = {`, `zh: {`, `applyEpubI18N()`, `micslangchange`} {
		if !strings.Contains(script, expected) {
			t.Errorf("EPUB reader localization is missing %q", expected)
		}
	}
	for _, expected := range []string{
		`App.prototype.showReaderLoading`,
		`App.prototype.hideReaderLoading`,
		`App.prototype.revealReader`,
		`App.prototype.displayInitialPage`,
		`this.state.rendition.on("displayed", this.onRenditionDisplayed.bind(this))`,
		`this.state.pageReady = true;`,
		`!this.state.pageReady`,
	} {
		if !strings.Contains(script, expected) {
			t.Errorf("EPUB reader page-load reveal logic is missing %q", expected)
		}
	}
	bookReadyStart := strings.Index(script, `App.prototype.onBookReady = function`)
	bookReadyEnd := strings.Index(script, `App.prototype.displayInitialPage = function`)
	if bookReadyStart < 0 || bookReadyEnd <= bookReadyStart {
		t.Error("EPUB reader book-ready/display initialization functions are missing or out of order")
	} else if strings.Contains(script[bookReadyStart:bookReadyEnd], `this.revealReader()`) {
		t.Error("book.ready must not reveal the reader before a page has been displayed")
	}
	if strings.Contains(script, `onRenditionStartedRestorePos`) {
		t.Error("saved-position restore must not start a second competing initial display")
	}
	if strings.Contains(script, "if (this.state.ttsTrackMode && this.state.ttsAutoNavigating)") {
		t.Error("long-track relocation must not restart audio after the display promise releases its navigation lock")
	}

	style := read("/static/reader/epub/style.css")
	for _, expected := range []string{".tts-options-panel", ".tts-stop-row", ".tts-wake-option", ".tts-action-menu"} {
		if !strings.Contains(style, expected) {
			t.Errorf("EPUB TTS styles are missing %q", expected)
		}
	}
	for _, expected := range []string{".app .reader-loading", ".app .reader-loading-card", ".app .reader-loading-spinner"} {
		if !strings.Contains(style, expected) {
			t.Errorf("EPUB reader loading styles are missing %q", expected)
		}
	}
	for _, opaque := range []string{"background: linear-gradient(145deg, #268fe0, #1268b6)", "background: rgba(24, 34, 45, 0.9)"} {
		if strings.Contains(style, opaque) {
			t.Errorf("floating TTS surface must remain transparent: %q", opaque)
		}
	}
	for _, opaque := range []string{"background: #ffffff", "touch-action: none"} {
		if !strings.Contains(style, opaque) {
			t.Errorf("TTS status bar must be a solid, draggable surface (missing %q)", opaque)
		}
	}

	worker := read("/sw.js")
	if !strings.Contains(worker, `CACHE_NAME = CACHE_PREFIX + "v25"`) {
		t.Error("PWA cache version was not advanced for the new reader assets")
	}
	if !strings.Contains(worker, `"/static/help.js"`) {
		t.Error("PWA app shell must cache the user guide script")
	}

	login := read("/login")
	for _, expected := range []string{`data-help-open`, `/static/help.js?v=`} {
		if !strings.Contains(login, expected) {
			t.Errorf("site pages are missing the help button script %q", expected)
		}
	}
	help := read("/api/help")
	for _, expected := range []string{`"title":"How to use this app"`, "Signing in", "Listen (read aloud)"} {
		if !strings.Contains(help, expected) {
			t.Errorf("user guide endpoint is missing %q", expected)
		}
	}

	tools := read("/static/reader-tools.js")
	for _, expected := range []string{"/api/reader/context", "Bookmark here", "Write note", "/api/about", "/api/help", "data-mics-help", "data-mics-home", "data-mics-menu", "data-mics-reader-about", "renderSettingsAbout", "mics-reader-header", "MicsBook menu", "setupToolsDrag", "micsReader:tools-position", "mics-reader-tools--floating", "mics-selbar", "translate.googleapis.com", "api.dictionaryapi.dev", "blockContextMenu"} {
		if !strings.Contains(tools, expected) {
			t.Errorf("reader tools are missing %q", expected)
		}
	}
}
