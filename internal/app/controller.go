package app

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "golang.org/x/image/webp"

	"anki-passive-widget/internal/anki"
	"anki-passive-widget/internal/config"
)

const (
	FrontWidth     = 360
	FrontHeight    = 220
	BackWidth      = 360
	BackHeight     = 310
	SettingsWidth  = 360
	SettingsHeight = 480
)

// DailyProgress tracks daily due/new review completion for the progress bar.
type DailyProgress struct {
	Date         string
	InitialTotal int
	Remaining    int
	Completed    int
	InitialDue   int
	RemainingDue int
	CompletedDue int
	InitialNew   int
	RemainingNew int
	CompletedNew int
}

// UIHooks receives controller state updates on background goroutines (window posts messages to its GUI thread).
type UIHooks interface {
	PostCardLoaded(card *anki.ParsedCard, progress DailyProgress)
	PostCardError(errMsg string)
	PostOpenSettings()
	PostPauseChanged(paused bool)
	PostWidgetHidden()
	PostQuit()
}

// Controller manages AnkiConnect/SQLite hybrid state, background scheduler, prefetching, and audio.
type Controller struct {
	ankiClient *anki.Client
	cfg        *config.Config
	audio      *audioPlayer
	hooks      UIHooks

	mu               sync.Mutex
	showMu           sync.Mutex
	ticker           *time.Ticker
	resetTimerCh     chan struct{}
	stopCh           chan struct{}
	snoozeTimer      *time.Timer
	paused           bool
	widgetVisible    bool
	currentCard       *anki.ParsedCard
	cardShownAt       time.Time
	prefetchedCard    *anki.ParsedCard
	prefetchDeck      string
	prefetchProfile   string
	prefetchStudyMode string
	prefetchColPath   string
	prefetchColMod    time.Time
	lastRemaining     int
	lastRemainingDue  int
	lastRemainingNew  int
	hasLastRemaining  bool
	shuttingDown      bool
}

func NewController(hooks UIHooks) *Controller {
	cfg, err := config.Load()
	if err != nil || cfg == nil {
		cfg = config.DefaultConfig()
	}

	c := &Controller{
		cfg:          cfg,
		ankiClient:   anki.NewClient(cfg.AnkiEndpoint),
		audio:        &audioPlayer{},
		hooks:        hooks,
		resetTimerCh: make(chan struct{}, 1),
		stopCh:       make(chan struct{}),
	}

	return c
}

func (c *Controller) Start() {
	go c.schedulerLoop()
	go func() {
		select {
		case <-c.stopCh:
			return
		case <-time.After(350 * time.Millisecond):
			if !c.IsPaused() {
				_, _ = c.ShowCardNow()
			} else {
				c.prefetchNextCard()
			}
		}
	}()
}

func (c *Controller) Shutdown() {
	c.mu.Lock()
	if c.shuttingDown {
		c.mu.Unlock()
		return
	}
	c.shuttingDown = true
	if c.snoozeTimer != nil {
		c.snoozeTimer.Stop()
		c.snoozeTimer = nil
	}
	close(c.stopCh)
	c.mu.Unlock()

	if c.audio != nil {
		c.audio.Stop()
	}
}

func (c *Controller) getMediaDir() string {
	c.mu.Lock()
	customDir := c.cfg.MediaDirPath
	selectedProfile := c.cfg.SelectedProfile
	c.mu.Unlock()

	if customDir != "" {
		return customDir
	}
	if prof, err := config.ResolveActiveProfile(selectedProfile); err == nil {
		return prof.MediaDirPath
	}
	return ""
}

func (c *Controller) activeCollectionPath() (string, error) {
	c.mu.Lock()
	selectedProfile := c.cfg.SelectedProfile
	c.mu.Unlock()

	prof, err := config.ResolveActiveProfile(selectedProfile)
	if err != nil {
		return "", err
	}
	return prof.CollectionPath, nil
}

func (c *Controller) pingAnkiConnect() (available bool, timedOut bool, err error) {
	if c.ankiClient == nil {
		return false, false, errors.New("anki client uninitialized")
	}
	if anki.IsLocalLoopbackEndpoint(c.ankiClient.Endpoint()) && !anki.IsAnkiDesktopRunning() {
		return false, false, nil
	}
	return c.ankiClient.PingStatus()
}

func (c *Controller) fetchNextCardHybrid(selectedDeck string) (*anki.ParsedCard, error) {
	c.mu.Lock()
	var frontFields, backFields []string
	studyMode := config.StudyModeBoth
	if c.cfg != nil {
		studyMode = c.cfg.StudyMode
		if c.cfg.DeckFields != nil {
			if sel, ok := c.cfg.DeckFields[selectedDeck]; ok {
				frontFields = append([]string(nil), sel.FrontFields...)
				backFields = append([]string(nil), sel.BackFields...)
			}
		}
	}
	c.mu.Unlock()

	avail, timedOut, _ := c.pingAnkiConnect()
	if avail {
		return c.ankiClient.FetchNextCardWithOptions(selectedDeck, frontFields, backFields, studyMode)
	}
	if timedOut {
		return nil, errors.New("AnkiConnect timed out")
	}
	colPath, err := c.activeCollectionPath()
	if err != nil {
		return nil, err
	}
	return anki.SQLiteFetchNextCardWithOptions(colPath, selectedDeck, frontFields, backFields, studyMode)
}

func (c *Controller) GetDeckFields(selectedDeck string) (*anki.DeckFieldsInfo, error) {
	avail, timedOut, _ := c.pingAnkiConnect()
	if avail {
		return c.ankiClient.GetDeckFields(selectedDeck)
	}
	if timedOut {
		return nil, errors.New("AnkiConnect timed out")
	}
	colPath, err := c.activeCollectionPath()
	if err != nil {
		return nil, err
	}
	return anki.SQLiteGetDeckFields(colPath, selectedDeck)
}

func (c *Controller) answerCardHybrid(cardID int64, ease int, elapsedMs int) error {
	avail, timedOut, _ := c.pingAnkiConnect()
	if avail {
		return c.ankiClient.AnswerCard(cardID, ease)
	}
	if timedOut || anki.IsAnkiDesktopRunning() {
		return errors.New("Anki Desktop is open but AnkiConnect is unreachable")
	}
	colPath, err := c.activeCollectionPath()
	if err != nil {
		return err
	}
	return anki.SQLiteAnswerCardWithElapsed(colPath, cardID, ease, elapsedMs)
}

func (c *Controller) GetDecks() ([]string, error) {
	c.mu.Lock()
	onlyDue := c.cfg.OnlyDueDecks
	c.mu.Unlock()
	return c.GetDecksFiltered(onlyDue)
}

func (c *Controller) GetDecksFiltered(onlyDue bool) ([]string, error) {
	avail, _, _ := c.pingAnkiConnect()
	if avail {
		return c.ankiClient.DeckNames(onlyDue)
	}
	colPath, err := c.activeCollectionPath()
	if err != nil {
		return nil, err
	}
	return anki.SQLiteDeckNames(colPath, onlyDue)
}

func (c *Controller) GetProfiles() ([]config.ProfileInfo, error) {
	return config.DiscoverProfiles()
}

func (c *Controller) GetDataSourceMode() string {
	if avail, _, _ := c.pingAnkiConnect(); avail {
		return "ankiconnect"
	}
	return "sqlite"
}

func (c *Controller) GetConfig() config.Config {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cfg == nil {
		return *config.DefaultConfig()
	}
	copyCfg := *c.cfg
	if c.cfg.DeckFields != nil {
		copyCfg.DeckFields = make(map[string]config.DeckFieldSelection, len(c.cfg.DeckFields))
		for k, v := range c.cfg.DeckFields {
			copyCfg.DeckFields[k] = config.DeckFieldSelection{
				FrontFields: append([]string(nil), v.FrontFields...),
				BackFields:  append([]string(nil), v.BackFields...),
			}
		}
	}
	return copyCfg
}

func (c *Controller) SaveConfig(newCfg config.Config) error {
	newCfg.Normalize()

	c.mu.Lock()
	if newCfg.DailyDate == "" {
		newCfg.DailyDate = c.cfg.DailyDate
		newCfg.DailyInitialTotal = c.cfg.DailyInitialTotal
		newCfg.DailyInitialDue = c.cfg.DailyInitialDue
		newCfg.DailyInitialNew = c.cfg.DailyInitialNew
	}
	if newCfg.SelectedDeck != c.cfg.SelectedDeck || newCfg.SelectedProfile != c.cfg.SelectedProfile {
		newCfg.DailyDate = ""
		newCfg.DailyInitialTotal = 0
		newCfg.DailyInitialDue = 0
		newCfg.DailyInitialNew = 0
		c.currentCard = nil
		c.cardShownAt = time.Time{}
		c.lastRemaining = 0
		c.lastRemainingDue = 0
		c.lastRemainingNew = 0
		c.hasLastRemaining = false
	} else {
		c.currentCard = nil
	}
	c.clearPrefetchLocked()
	c.cfg = &newCfg
	c.ankiClient.SetEndpoint(newCfg.AnkiEndpoint)
	c.mu.Unlock()

	if err := config.Save(&newCfg); err != nil {
		return err
	}
	c.resetScheduler()
	return nil
}

func (c *Controller) PlayAudioFile(filename string) {
	if filename == "" || c.audio == nil {
		return
	}
	go func() {
		diskPath, data, name, err := c.ankiClient.ResolveMediaFile(filename, c.getMediaDir)
		if err != nil {
			return
		}
		c.audio.Play(diskPath, data, name)
	}()
}

func (c *Controller) StopAudio() {
	if c.audio != nil {
		c.audio.Stop()
	}
}

// LoadImageDIB resolves an Anki media image src (/media/...) and decodes it to top-down BGRA pixels.
func (c *Controller) LoadImageDIB(src string) (*DecodedDIB, error) {
	diskPath, data, _, err := c.ankiClient.ResolveMediaFile(src, c.getMediaDir)
	if err != nil {
		return nil, err
	}
	if diskPath != "" {
		data, err = os.ReadFile(filepath.Clean(diskPath))
		if err != nil {
			return nil, err
		}
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	return imageToBGRA(img), nil
}

func (c *Controller) schedulerLoop() {
	interval := c.getIntervalDuration()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-c.stopCh:
			return
		case <-c.resetTimerCh:
			ticker.Reset(c.getIntervalDuration())
		case <-ticker.C:
			if c.IsPaused() || c.IsWidgetVisible() {
				continue
			}
			_, _ = c.ShowCardNow()
		}
	}
}

func (c *Controller) getIntervalDuration() time.Duration {
	c.mu.Lock()
	mins := c.cfg.IntervalMinutes
	c.mu.Unlock()
	if mins < config.MinIntervalMinutes {
		mins = config.DefaultIntervalMinutes
	}
	return time.Duration(mins) * time.Minute
}

func (c *Controller) resetScheduler() {
	select {
	case c.resetTimerCh <- struct{}{}:
	default:
	}
}

func (c *Controller) IsWidgetVisible() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.widgetVisible
}

func (c *Controller) clearPrefetchLocked() {
	c.prefetchedCard = nil
	c.prefetchDeck = ""
	c.prefetchProfile = ""
	c.prefetchStudyMode = ""
	c.prefetchColPath = ""
	c.prefetchColMod = time.Time{}
}

func (c *Controller) takePrefetchedCard(selectedDeck, selectedProfile, studyMode string) *anki.ParsedCard {
	c.mu.Lock()
	card := c.prefetchedCard
	deck := c.prefetchDeck
	profile := c.prefetchProfile
	mode := c.prefetchStudyMode
	colPath := c.prefetchColPath
	colMod := c.prefetchColMod
	c.clearPrefetchLocked()
	c.mu.Unlock()

	if card == nil || deck != selectedDeck || profile != selectedProfile || mode != studyMode {
		return nil
	}
	if anki.IsAnkiDesktopRunning() || colPath == "" {
		return nil
	}
	if st, err := os.Stat(colPath); err != nil || !st.ModTime().Equal(colMod) {
		return nil
	}
	return card
}

func (c *Controller) prefetchNextCard() {
	c.mu.Lock()
	if c.shuttingDown || c.cfg == nil {
		c.mu.Unlock()
		return
	}
	selectedDeck := c.cfg.SelectedDeck
	selectedProfile := c.cfg.SelectedProfile
	studyMode := c.cfg.StudyMode
	c.mu.Unlock()

	if anki.IsAnkiDesktopRunning() {
		return
	}
	colPath, err := c.activeCollectionPath()
	if err != nil || colPath == "" {
		return
	}
	stBefore, err := os.Stat(colPath)
	if err != nil {
		return
	}
	card, err := c.fetchNextCardHybrid(selectedDeck)
	if err != nil || card == nil {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.shuttingDown || c.cfg == nil || c.cfg.SelectedDeck != selectedDeck || c.cfg.SelectedProfile != selectedProfile || c.cfg.StudyMode != studyMode {
		return
	}
	c.prefetchedCard = card
	c.prefetchDeck = selectedDeck
	c.prefetchProfile = selectedProfile
	c.prefetchStudyMode = studyMode
	c.prefetchColPath = colPath
	c.prefetchColMod = stBefore.ModTime()
}

func (c *Controller) ShowCardNow() (*anki.ParsedCard, error) {
	c.showMu.Lock()
	defer c.showMu.Unlock()

	c.mu.Lock()
	selectedDeck := c.cfg.SelectedDeck
	selectedProfile := c.cfg.SelectedProfile
	studyMode := c.cfg.StudyMode
	alreadyVisible := c.widgetVisible
	activeCard := c.currentCard
	if c.snoozeTimer != nil {
		c.snoozeTimer.Stop()
		c.snoozeTimer = nil
	}
	c.mu.Unlock()

	if alreadyVisible && activeCard != nil && activeCard.CardID > 0 {
		prog := c.GetDailyProgress()
		if c.hooks != nil {
			c.hooks.PostCardLoaded(activeCard, prog)
		}
		return activeCard, nil
	}

	card := c.takePrefetchedCard(selectedDeck, selectedProfile, studyMode)
	if card == nil {
		var err error
		card, err = c.fetchNextCardHybrid(selectedDeck)
		if err != nil {
			c.mu.Lock()
			c.widgetVisible = true
			c.mu.Unlock()
			if c.hooks != nil {
				c.hooks.PostCardError(err.Error())
			}
			return nil, err
		}
	}

	prog := c.updateDailyProgress(card.RemainingDue, card.RemainingNew, studyMode)

	c.mu.Lock()
	c.currentCard = card
	c.cardShownAt = time.Now()
	c.lastRemaining = card.TotalRemaining
	c.lastRemainingDue = card.RemainingDue
	c.lastRemainingNew = card.RemainingNew
	c.hasLastRemaining = true
	c.widgetVisible = true
	c.mu.Unlock()

	if c.hooks != nil {
		c.hooks.PostCardLoaded(card, prog)
	}
	return card, nil
}

func (c *Controller) OpenSettings() {
	c.mu.Lock()
	c.widgetVisible = true
	c.mu.Unlock()
	if c.hooks != nil {
		c.hooks.PostOpenSettings()
	}
}

func (c *Controller) SubmitAnswer(cardID int64, ease int) error {
	c.mu.Lock()
	shownAt := c.cardShownAt
	activeCard := c.currentCard
	c.mu.Unlock()

	if activeCard != nil && activeCard.CardID > 0 && activeCard.CardID != cardID {
		return errors.New("card updated; please retry")
	}

	elapsedMs := 8000
	if !shownAt.IsZero() {
		elapsedMs = int(time.Since(shownAt).Milliseconds())
	}

	if err := c.answerCardHybrid(cardID, ease, elapsedMs); err != nil {
		if anki.IsQueueMismatchError(err) {
			c.mu.Lock()
			c.currentCard = nil
			c.cardShownAt = time.Time{}
			c.clearPrefetchLocked()
			c.mu.Unlock()
			return errors.New("card is not at top of Anki queue; click Retry")
		}
		return err
	}

	c.mu.Lock()
	if c.currentCard != nil {
		remaining := c.currentCard.TotalRemaining - 1
		if remaining < 0 {
			remaining = 0
		}
		remDue := c.currentCard.RemainingDue
		remNew := c.currentCard.RemainingNew
		if c.currentCard.IsDue {
			if remDue > 0 {
				remDue--
			}
		} else {
			if remNew > 0 {
				remNew--
			}
		}
		c.currentCard.RemainingDue = remDue
		c.currentCard.RemainingNew = remNew
		c.currentCard.TotalRemaining = remaining
		c.lastRemaining = remaining
		c.lastRemainingDue = remDue
		c.lastRemainingNew = remNew
		c.hasLastRemaining = true
	}
	c.currentCard = nil
	c.cardShownAt = time.Time{}
	c.widgetVisible = false
	c.clearPrefetchLocked()
	c.mu.Unlock()

	c.StopAudio()
	if c.hooks != nil {
		c.hooks.PostWidgetHidden()
	}
	c.resetScheduler()
	go c.prefetchNextCard()
	c.scheduleIdleMemoryTrim()
	return nil
}

func (c *Controller) Snooze(minutes int) {
	if minutes <= 0 {
		minutes = 5
	}

	c.mu.Lock()
	c.widgetVisible = false
	if c.snoozeTimer != nil {
		c.snoozeTimer.Stop()
	}
	c.snoozeTimer = time.AfterFunc(time.Duration(minutes)*time.Minute, func() {
		if !c.IsPaused() && !c.IsWidgetVisible() {
			_, _ = c.ShowCardNow()
		}
	})
	c.mu.Unlock()

	c.StopAudio()
	if c.hooks != nil {
		c.hooks.PostWidgetHidden()
	}
	c.scheduleIdleMemoryTrim()
}

func (c *Controller) HideWidget() {
	c.mu.Lock()
	c.widgetVisible = false
	c.mu.Unlock()

	c.StopAudio()
	if c.hooks != nil {
		c.hooks.PostWidgetHidden()
	}
	c.resetScheduler()
	c.scheduleIdleMemoryTrim()
}

func (c *Controller) scheduleIdleMemoryTrim() {
	go func() {
		select {
		case <-c.stopCh:
			return
		case <-time.After(300 * time.Millisecond):
		}
		if c.IsWidgetVisible() {
			return
		}
		trimIdleProcessMemory()
	}()
}

func (c *Controller) TogglePause() bool {
	c.mu.Lock()
	c.paused = !c.paused
	paused := c.paused
	c.mu.Unlock()

	if c.hooks != nil {
		c.hooks.PostPauseChanged(paused)
	}
	return paused
}

func (c *Controller) IsPaused() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.paused
}

func (c *Controller) currentCollectionDay() string {
	if colPath, err := c.activeCollectionPath(); err == nil {
		if day, dayErr := anki.SQLiteCollectionDay(colPath); dayErr == nil && day != "" {
			return day
		}
	}
	return time.Now().Add(-4 * time.Hour).Format("2006-01-02")
}

func (c *Controller) GetDailyProgress() DailyProgress {
	today := c.currentCollectionDay()

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cfg.DailyDate != "" && c.cfg.DailyDate != today {
		return buildDailyProgress(today, 0, 0, 0, 0)
	}
	remDue, remNew := 0, 0
	switch {
	case c.currentCard != nil:
		remDue = c.currentCard.RemainingDue
		remNew = c.currentCard.RemainingNew
	case c.hasLastRemaining:
		remDue = c.lastRemainingDue
		remNew = c.lastRemainingNew
	case c.cfg.DailyDate == today:
		remDue = c.cfg.DailyInitialDue
		remNew = c.cfg.DailyInitialNew
	}
	return buildDailyProgress(c.cfg.DailyDate, c.cfg.DailyInitialDue, remDue, c.cfg.DailyInitialNew, remNew)
}

func (c *Controller) updateDailyProgress(remDue, remNew int, studyMode string) DailyProgress {
	today := c.currentCollectionDay()

	c.mu.Lock()
	changed := false
	if c.cfg.DailyDate != today {
		c.cfg.DailyDate = today
		c.cfg.DailyInitialDue = 0
		c.cfg.DailyInitialNew = 0
		c.cfg.DailyInitialTotal = 0
		changed = true
	}
	if studyMode != config.StudyModeNew && remDue > c.cfg.DailyInitialDue {
		c.cfg.DailyInitialDue = remDue
		changed = true
	}
	if studyMode != config.StudyModeDue && remNew > c.cfg.DailyInitialNew {
		c.cfg.DailyInitialNew = remNew
		changed = true
	}
	desiredTotal := c.cfg.DailyInitialDue + c.cfg.DailyInitialNew
	if desiredTotal != c.cfg.DailyInitialTotal {
		c.cfg.DailyInitialTotal = desiredTotal
		changed = true
	}
	snapshot := *c.cfg
	c.mu.Unlock()

	if changed {
		_ = config.Save(&snapshot)
	}
	return buildDailyProgress(today, snapshot.DailyInitialDue, remDue, snapshot.DailyInitialNew, remNew)
}

func buildDailyProgress(date string, initDue, remDue, initNew, remNew int) DailyProgress {
	if remDue < 0 {
		remDue = 0
	}
	if initDue < remDue {
		initDue = remDue
	}
	completedDue := initDue - remDue
	if completedDue < 0 {
		completedDue = 0
	}

	if remNew < 0 {
		remNew = 0
	}
	if initNew < remNew {
		initNew = remNew
	}
	completedNew := initNew - remNew
	if completedNew < 0 {
		completedNew = 0
	}

	return DailyProgress{
		Date:         date,
		InitialTotal: initDue + initNew,
		Remaining:    remDue + remNew,
		Completed:    completedDue + completedNew,
		InitialDue:   initDue,
		RemainingDue: remDue,
		CompletedDue: completedDue,
		InitialNew:   initNew,
		RemainingNew: remNew,
		CompletedNew: completedNew,
	}
}

// DecodedDIB holds a 32-bit BGRA bitmap ready for fast Win32 StretchDIBits rendering.
type DecodedDIB struct {
	Width  int
	Height int
	Pixels []byte // top-down BGRA
}

func imageToBGRA(src image.Image) *DecodedDIB {
	b := src.Bounds()
	w := b.Dx()
	h := b.Dy()
	if w <= 0 || h <= 0 {
		return nil
	}
	// Cap huge images in-memory to max 1400px longest side to keep RAM tiny (<8MB).
	const maxDim = 1400
	stepX, stepY := 1, 1
	for (w / stepX) > maxDim {
		stepX++
	}
	for (h / stepY) > maxDim {
		stepY++
	}
	outW := (w + stepX - 1) / stepX
	outH := (h + stepY - 1) / stepY
	pix := make([]byte, outW*outH*4)

	// Composite transparent pixels over crisp light card paper (#F8FAFC) so dark ink screenshots/kanji remain legible.
	const bgR, bgG, bgB = 248, 250, 252
	idx := 0
	for oy := 0; oy < outH; oy++ {
		sy := b.Min.Y + oy*stepY
		for ox := 0; ox < outW; ox++ {
			sx := b.Min.X + ox*stepX
			c := color.NRGBAModel.Convert(src.At(sx, sy)).(color.NRGBA)
			a := int(c.A)
			invA := 255 - a
			r := (int(c.R)*a + bgR*invA) / 255
			g := (int(c.G)*a + bgG*invA) / 255
			bl := (int(c.B)*a + bgB*invA) / 255
			pix[idx+0] = byte(bl)
			pix[idx+1] = byte(g)
			pix[idx+2] = byte(r)
			pix[idx+3] = 255
			idx += 4
		}
	}
	return &DecodedDIB{
		Width:  outW,
		Height: outH,
		Pixels: pix,
	}
}

