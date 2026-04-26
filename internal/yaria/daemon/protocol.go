package daemon

// IPC command names
const (
	CmdAdd    = "add"
	CmdRemove = "remove"
	CmdPause  = "pause"
	CmdResume = "resume"
	CmdList   = "list"
	CmdStop   = "stop"
)

// Request is sent from the TUI/CLI to the daemon
type Request struct {
	Cmd   string `json:"cmd"`
	ID    string `json:"id,omitempty"`
	URL   string `json:"url,omitempty"`
	Title string `json:"title,omitempty"`
	Dir   string `json:"dir,omitempty"`
	// Download config
	IsAudioOnly    bool   `json:"is_audio_only,omitempty"`
	AudioFormat    string `json:"audio_format,omitempty"`
	Resolution     string `json:"resolution,omitempty"`
	CookieBrowser  string `json:"cookie_browser,omitempty"`
	UseAria2c      bool   `json:"use_aria2c,omitempty"`
	Aria2cArgs     string `json:"aria2c_args,omitempty"`
	OutputTemplate string `json:"output_template,omitempty"`
}

// Response is sent from the daemon back to the caller
type Response struct {
	OK       bool           `json:"ok"`
	Error    string         `json:"error,omitempty"`
	Torrents []DownloadInfo `json:"torrents,omitempty"`
}

// DownloadInfo represents the status of a single download
type DownloadInfo struct {
	ID        string  `json:"id"`
	URL       string  `json:"url"`
	Title     string  `json:"title"`
	Dir       string  `json:"dir"`
	State     string  `json:"state"` // "downloading", "paused", "complete", "error", "preparing"
	Percent   float64 `json:"percent"`
	Speed     string  `json:"speed"`
	ETA       string  `json:"eta"`
	Error     string  `json:"error,omitempty"`
	StatusMsg string  `json:"status_msg,omitempty"` // current activity text
}
