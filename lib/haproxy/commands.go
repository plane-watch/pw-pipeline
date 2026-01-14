package haproxy

const (
	cmdAddMap          = `add map %s %s %s`
	cmdClearTableByKey = "clear table %s key %s"
	cmdDelMap          = `del map %s %s`
	cmdEcho            = "echo"
	cmdGetMap          = "get map %s %s"
	cmdPrompt          = "prompt"
	cmdSetMap          = `set map %s %s %s`
	cmdSetTable        = "set table %s key %s"
	cmdShowInfoTyped   = "show info typed"
	cmdShowMap         = "show map %s"
	cmdShowMaps        = "show map"
	cmdShowSess        = "show sess"
	cmdShowTable       = "show table %s"
	cmdShowTables      = "show table"
	cmdShutdownSession = "shutdown session %s"
)
