package beam

const (
	Empty = ""
)

// Status constants for response states.
const (
	StatusError      = "-error"
	StatusPending    = "?pending"
	StatusSuccessful = "+ok"
	StatusFatal      = "*fatal"
)

// Header information
const (
	HeaderPrefix      = "X-Beam"
	HeaderContentType = "Content-Type"

	HeaderNameDuration  = "Duration"
	HeaderNameTimestamp = "Timestamp"
	HeaderNameApp       = "App"
	HeaderNameServer    = "Server"
	HeaderNameVersion   = "Version"
	HeaderNameBuild     = "Build"
	HeaderNamePlay      = "Play"
)

// -----------------------------------------------------------------------------
// System Metadata and Renderer Settings
// -----------------------------------------------------------------------------

// SystemShow controls where system info is displayed.
type SystemShow int

const (
	SystemShowNone SystemShow = iota
	SystemShowHeaders
	SystemShowBody
	SystemShowBoth
)
