package haproxy

import "regexp"

var (

	// reShowInfo is a regular expression that splits each line of the "show info typed"
	// output into the following capture groups:
	//
	//  - Group 1: the numeric position of the field in the list starting at zero
	//  - Group 2: the field name
	//  - Group 3: the process number starting at 1 (legacy)
	//  - Group 4: three letters that correspond to the field’s origin, nature, and scope of the variable
	//  - Group 5: the field’s type (e.g. str for string and u32 for unsigned 32-bit integer)
	//  - Group 6: the value itself
	//
	// See: https://www.haproxy.com/documentation/haproxy-runtime-api/reference/show-info/#typed-format
	reShowInfo = regexp.MustCompile(`^(\d+)\.([A-Za-z0-9_\- ]+)\.(\d+):(\w+):(\w+):(.*)$`)

	// reShowMaps is a regular expression that splits each line of the "show map"
	// output into the following capture groups:
	//
	//  - Group 1: the numeric id of the map
	//  - Group 2: the file representing the map
	//  - Group 3: the map description
	//  - Group 4: the map counters
	//
	// See: https://www.haproxy.com/documentation/haproxy-runtime-api/reference/show-map/
	reShowMaps = regexp.MustCompile(`^(\d+)\s\((.*?)\)\s+(.*?)\.\s+(.*?)$`)

	// reShowMap is a regular expression that splits each line of the "show map <mapname>"
	// output into the following capture groups:
	//
	//  - Group 1: the key
	//  - Group 2: the value
	//
	// See: https://www.haproxy.com/documentation/haproxy-runtime-api/reference/show-map/
	reShowMap = regexp.MustCompile(`^0[xX][[:xdigit:]]{12}\s+([[:xdigit:]]{8}-[[:xdigit:]]{4}-[[:xdigit:]]{4}-[[:xdigit:]]{4}-[[:xdigit:]]{12})\s+(.*)$`)

	reShowMapsCurrVer  = regexp.MustCompile(`^curr_ver=(\d+)$`)
	reShowMapsNextVer  = regexp.MustCompile(`^next_ver=(\d+)$`)
	reShowMapsEntryCnt = regexp.MustCompile(`^entry_cnt=(\d+)$`)

	reGetMapType  = regexp.MustCompile(`^type="?(.*?)"?$`)
	reGetMapCase  = regexp.MustCompile(`^case=(.*?)$`)
	reGetMapFound = regexp.MustCompile(`^found=(.*?)$`)
	reGetMapKey   = regexp.MustCompile(`^key="(.*?)"$`)
	reGetMapValue = regexp.MustCompile(`^value="(.*?)"$`)

	reShowSessID    = regexp.MustCompile(`^(0[xX][[:xdigit:]]{12}):\s+`)
	reShowSessProto = regexp.MustCompile(`^proto=(.*?)$`)
	reShowSessSrc   = regexp.MustCompile(`^src=(.*?)$`)
	reShowSessFe    = regexp.MustCompile(`^fe=(.*?)$`)
	reShowSessBe    = regexp.MustCompile(`^be=(.*?)$`)
	reShowSessSrv   = regexp.MustCompile(`^srv=(.*?)$`)
	reShowSessAge   = regexp.MustCompile(`^age=(.*?)$`)

	// reShowTables is a regular expression (duh) that splits each line of the "show table"
	// output into the following capture groups:
	//
	//  - Group 1: the table name
	//  - Group 2: the table type
	//  - Group 3: the table size
	//  - Group 4: the table used
	//
	// See: https://www.haproxy.com/documentation/haproxy-runtime-api/reference/show-table/
	reShowTables = regexp.MustCompile(`^#\s+table:\s+(\w+),\s+type:\s+(\w+),\s+size:(\d+),\s+used:(\d+)$`)

	reShowTableLine = regexp.MustCompile(`^0[xX][[:xdigit:]]+:\s+key=(.*?)\s+(.*)$`)
)
