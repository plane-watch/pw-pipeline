package main

import (
	"math"
	"strconv"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/urfave/cli/v2"
	"plane.watch/lib/export"
)

const (
	planesSourceIncoming planesSource = iota
	planesSourceIngest
	planesSourceEnriched
	planesSourceRoutedLow
	planesSourceRoutedHigh
	planesSourceWSLow
	planesSourceWSHigh
)

type keyMap map[string]key.Binding

var keyBindings = keyMap{
	"Up":       key.NewBinding(key.WithKeys("up"), key.WithHelp("↑", "Move up in the aircraft list")),
	"Down":     key.NewBinding(key.WithKeys("up"), key.WithHelp("↓", "Move up in the aircraft list")),
	"PageUp":   key.NewBinding(key.WithKeys("up"), key.WithHelp("PgUp", "Move a page up in the aircraft list")),
	"PageDown": key.NewBinding(key.WithKeys("up"), key.WithHelp("PgDn", "Move a page down in the aircraft list")),

	"Source": key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "Switch Plane List Data Source")),
	"Select": key.NewBinding(key.WithKeys(tea.KeyEnter.String()), key.WithHelp(tea.KeyEnter.String(), "Select a plane")),

	"Quit": key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q/ctrl+c", "Exit")),

	"Help": key.NewBinding(key.WithKeys("h", "?"), key.WithHelp("h/?", "Show Help")),
}

func runTui(c *cli.Context) error {
	m, err := initialModel(c.String(natsURL), c.String(websocketURL))
	if err != nil {
		return err
	}
	defer m.tapper.Disconnect()

	filterIcao := c.String(icao)
	filterFeeder := c.String(feederAPIKey)
	if err = m.tapper.IncomingDataTap(filterIcao, filterFeeder, m.handleIncomingData); err != nil {
		return err
	}

	if err = m.tapper.AfterIngestTap(filterIcao, filterFeeder, m.afterIngest.update); err != nil {
		return err
	}
	if err = m.tapper.AfterEnrichmentTap(filterIcao, filterFeeder, m.afterEnrichment.update); err != nil {
		return err
	}
	if err = m.tapper.AfterRouterLowTap(filterIcao, filterFeeder, m.afterRouterLow.update); err != nil {
		return err
	}
	if err = m.tapper.AfterRouterHighTap(filterIcao, filterFeeder, m.afterRouterHigh.update); err != nil {
		return err
	}
	if err = m.tapper.WebSocketTapLow(filterIcao, filterFeeder, m.finalLow.update); err != nil {
		return err
	}
	if err = m.tapper.WebSocketTapHigh(filterIcao, filterFeeder, m.finalHigh.update); err != nil {
		return err
	}

	if _, err = tea.NewProgram(m, tea.WithAltScreen()).Run(); nil != err {
		return err
	}
	return nil
}

func (m *model) Init() tea.Cmd {
	return m.tickCmd()
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch tMsg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(tMsg, keyBindings["Quit"]):
			return m, tea.Quit
		case key.Matches(tMsg, keyBindings["Source"]):
			if m.source == planesSourceWSHigh {
				m.source = planesSourceIngest
			} else {
				m.source++
			}
		case key.Matches(tMsg, keyBindings["Select"]):
			m.selectedIcao = m.planesTable.SelectedRow()[0]
			m.selectedCallSign = m.planesTable.SelectedRow()[2]
			m.logger.Info().
				Str("icao", m.selectedIcao).
				Str("callsign", m.selectedCallSign).
				Msg("Selecting Aircraft")
		case key.Matches(tMsg, keyBindings["Help"]):
			m.help.ShowAll = !m.help.ShowAll
		}
	case tea.WindowSizeMsg:
		m.width = tMsg.Width
		m.height = tMsg.Height
		m.handleWindowSizing()

		return m, nil
	case timerTick:
		m.tickCount++
		m.updateIncomingStats()
		m.updateAircraftTable()

		m.updateSelectedAircraftTable()
		return m, m.tickCmd()
	}
	var cmd1, cmd2, cmd3 tea.Cmd
	m.statsTable, cmd1 = m.statsTable.Update(msg)
	m.selectedTable, cmd2 = m.selectedTable.Update(msg)
	m.planesTable, cmd3 = m.planesTable.Update(msg)

	return m, tea.Batch(cmd1, cmd2, cmd3)
}

func (m *model) handleWindowSizing() {
	m.statsTable.SetWidth(m.width)
	m.selectedTable.SetWidth(m.width)
	m.planesTable.SetWidth(m.width)
	m.help.Width = m.width

	headingHeight := lipgloss.Height(m.heading.Render("test"))
	statsTableHeight := 3
	selectedTableHeight := 9

	planeViewTop := statsTableHeight + selectedTableHeight + (headingHeight * 3)

	planesViewHeight := 20
	if planesViewHeight+planeViewTop > m.height {
		planesViewHeight = min(0, m.height-planeViewTop)
	}
	m.planesTable.SetHeight(planesViewHeight)

	logViewTop := statsTableHeight + selectedTableHeight + planesViewHeight + (headingHeight * 4)
	logViewHeight := 15
	if logViewHeight+logViewTop > m.height {
		logViewHeight = min(0, m.height-logViewTop)
	}

	if !m.logViewReady {
		// configure log viewport
		m.logViewReady = true
		m.logView = viewport.New(m.width, logViewHeight)
		m.logView.YPosition = logViewTop
	} else {
		m.logView.Width = m.width
		m.logView.Height = logViewHeight
	}
}

func (m *model) updateIncomingStats() {
	m.incomingMutex.Lock()
	defer m.incomingMutex.Unlock()

	m.statsTable.SetRows([]table.Row{
		{
			// Number of feeders
			strconv.Itoa(len(m.feederSources)),
			// Number of Planes
			strconv.Itoa(len(m.incomingIcaoFrames)),

			// source frame type counts
			strconv.FormatUint(m.numIncomingBeast, 10),
			strconv.FormatUint(m.numIncomingAvr, 10),
			strconv.FormatUint(m.numIncomingSbs1, 10),

			// Ingest parsed out frames
			m.afterIngest.numFrames(),

			// enriched frames
			m.afterEnrichment.numFrames(),

			// routed low
			m.afterRouterLow.numFrames(),
			// routed high
			m.afterRouterHigh.numFrames(),

			// websocket low
			m.finalLow.numFrames(),
			m.finalHigh.numFrames(),
		},
	})
}

func (m *model) updateSelectedAircraftTable() {
	if m.selectedIcao == "" {
		m.selectedTable.SetRows(m.defaultSelectedTableRows())
		return
	}

	m.selectedTable.SetRows([]table.Row{
		m.selectedTableIncomingRow(),
		m.selectedTableRow(planesSourceIngest, &m.afterIngest),
		m.selectedTableRow(planesSourceEnriched, &m.afterEnrichment),
		m.selectedTableRow(planesSourceRoutedLow, &m.afterRouterLow),
		m.selectedTableRow(planesSourceRoutedHigh, &m.afterRouterHigh),
		m.selectedTableRow(planesSourceWSLow, &m.finalLow),
		m.selectedTableRow(planesSourceWSHigh, &m.finalHigh),
	})
}

func (m *model) selectedTableIncomingRow() table.Row {
	m.incomingMutex.Lock()
	defer m.incomingMutex.Unlock()
	icaoInt, err := strconv.ParseUint(m.selectedIcao, 16, 32)
	row := m.defaultSelectedTableRows()[0]
	if nil != err {
		return row
	}
	row[1] = strconv.Itoa(m.incomingIcaoFrames[uint32(icaoInt)])
	return row
}
func (m *model) selectedTableRow(source planesSource, data *sourceInfo) table.Row {
	loc := data.getLoc(m.selectedIcao)
	return table.Row{
		source.String(),
		data.numFramesFor(m.selectedIcao),
		loc.SquawkStr(),
		loc.LatStr(),
		loc.LonStr(),
		loc.AltitudeStr(),
		loc.VerticalRateStr(),
		loc.HeadingStr(),
	}
}

func (m *model) currentSourceData() *sourceInfo {
	switch m.source {
	case planesSourceIngest:
		return &m.afterIngest
	case planesSourceEnriched:
		return &m.afterEnrichment
	case planesSourceRoutedLow:
		return &m.afterRouterLow
	case planesSourceRoutedHigh:
		return &m.afterRouterHigh
	case planesSourceWSLow:
		return &m.finalLow
	case planesSourceWSHigh:
		return &m.finalHigh
	}
	return nil
}

func (m *model) updateAircraftTable() {
	data := m.currentSourceData()

	if nil == data {
		m.planesTable.SetRows([]table.Row{})
		return
	}
	data.mu.Lock()
	defer data.mu.Unlock()

	rows := make([]table.Row, 0, len(data.planes))
	var p *export.PlaneLocation
	for _, icaoStr := range data.icaos {
		p = data.planes[icaoStr]
		rows = append(rows, table.Row{
			icaoStr,
			strconv.Itoa(len(p.SourceTags)),
			p.CallSignStr(),
			p.SquawkStr(),
			p.LatStr(),
			p.LonStr(),
			p.AltitudeStr(),
			p.VerticalRateStr(),
			p.HeadingStr(),
		})
	}
	m.planesTable.SetRows(rows)
}

func (m *model) View() string {
	if m.logViewReady && m.logView.Height > 0 {
		m.logView.SetContent(m.logs.String())
		m.logView.SetYOffset(math.MaxInt)
	}

	view := m.heading.Render("Received Data Stats") + "\n" +
		m.statsTable.View() + "\n" +
		m.heading.Render("Selected Aircraft "+m.selectedIcao+" "+m.selectedCallSign) + "\n" +
		m.selectedTable.View() + "\n"

	view += m.heading.Render("All Aircraft - Source: "+m.source.String()) + "\n"
	view += m.planesTable.View() + "\n"

	if m.logViewReady && m.logView.Height > 0 {
		view += m.heading.Render("Logs") + "\n"
		view += m.logView.View() + "\n"
	}

	view += m.help.View(keyBindings)
	return view
}

func (m *model) tickCmd() tea.Cmd {
	return tea.Tick(m.tickDuration, func(t time.Time) tea.Msg {
		return timerTick(t)
	})
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k["Help"], k["Quit"]}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k["Up"], k["Down"], k["PgUp"], k["PgDn"]},
		{k["Source"], k["Select"]},
		{k["Help"], k["Quit"]},
	}
}
