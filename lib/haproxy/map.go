package haproxy

import (
	"fmt"
	"strconv"
	"strings"
)

type (
	MapResult struct {
		ID             int
		File           string
		Description    string
		CurrentVersion int
		NextVersion    int
		EntryCount     int
	}

	MapEntryResult struct {
		Type           string
		Case           string
		Found          bool
		Key            string
		Value          string
		foundSomething bool
	}
)

func (hap *HAProxy) DelMap(mapName, key string) error {
	out, err := hap.Command(fmt.Sprintf(cmdDelMap, mapName, key))
	if err != nil {
		return err
	}

	var output string

	for _, line := range strings.Split(out, "\n") {
		// remove leading & trailing spaces
		line = strings.TrimSpace(line)

		// skip blank lines
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "#") {
			continue
		}

		output = strings.Join([]string{line}, " ")
		return fmt.Errorf(output)
	}
	return nil
}

func (hap *HAProxy) AddMap(mapName, key, value string) error {
	return hap.addSetMap(mapName, key, value, cmdAddMap)
}

func (hap *HAProxy) SetMap(mapName, key, value string) error {
	return hap.addSetMap(mapName, key, value, cmdSetMap)
}

func (hap *HAProxy) addSetMap(mapName, key, value, cmd string) error {
	out, err := hap.Command(fmt.Sprintf(cmd, mapName, key, value))
	if err != nil {
		return err
	}

	var output string

	for _, line := range strings.Split(out, "\n") {
		// remove leading & trailing spaces
		line = strings.TrimSpace(line)

		// skip blank lines
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "#") {
			continue
		}

		output = strings.Join([]string{line}, " ")
		return fmt.Errorf(output)
	}
	return nil
}

func (hap *HAProxy) GetMap(mapName, key string) ([]MapEntryResult, error) {
	out, err := hap.Command(fmt.Sprintf(cmdGetMap, mapName, key))
	if err != nil {
		return nil, err
	}

	output := make([]MapEntryResult, 0)

	for _, line := range strings.Split(out, "\n") {
		// remove leading & trailing spaces
		line = strings.TrimSpace(line)

		// skip blank lines
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "#") {
			continue
		}

		mer := MapEntryResult{}

		for _, kv := range strings.Split(line, ",") {
			kv = strings.TrimSpace(kv)
			switch {
			case reGetMapType.MatchString(kv):
				matches := reGetMapType.FindStringSubmatch(kv)
				if len(matches) != 2 {
					return nil, fmt.Errorf("could not parse map type: %s", kv)
				}
				mer.Type = matches[1]
				mer.foundSomething = true
			case reGetMapCase.MatchString(kv):
				matches := reGetMapCase.FindStringSubmatch(kv)
				if len(matches) != 2 {
					return nil, fmt.Errorf("could not parse map case: %s", kv)
				}
				mer.Case = matches[1]
				mer.foundSomething = true
			case reGetMapFound.MatchString(kv):
				matches := reGetMapFound.FindStringSubmatch(kv)
				if len(matches) != 2 {
					return nil, fmt.Errorf("could not parse map found: %s", kv)
				}
				if matches[1] == "yes" {
					mer.Found = true
				}
				mer.foundSomething = true
			case reGetMapKey.MatchString(kv):
				matches := reGetMapKey.FindStringSubmatch(kv)
				if len(matches) != 2 {
					return nil, fmt.Errorf("could not parse map key: %s", kv)
				}
				mer.Key = matches[1]
				mer.foundSomething = true
			case reGetMapValue.MatchString(kv):
				matches := reGetMapValue.FindStringSubmatch(kv)
				if len(matches) != 2 {
					return nil, fmt.Errorf("could not parse map value: %s", kv)
				}
				mer.Value = matches[1]
				mer.foundSomething = true
			}
		}
		if mer.foundSomething && mer.Found {
			output = append(output, mer)
		}
	}
	return output, nil
}

func (hap *HAProxy) ShowMap(mapName string) (map[string]string, error) {
	var output map[string]string

	maps, err := hap.ShowMaps()
	if err != nil {
		return nil, fmt.Errorf("error getting maps: %v", err)
	}

	found := false
	for _, m := range maps {
		if m.File == mapName {
			found = true
			output = make(map[string]string, m.EntryCount)
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("map %s not found", mapName)
	}

	out, err := hap.Command(fmt.Sprintf(cmdShowMap, mapName))
	if err != nil {
		return nil, err
	}

	for _, line := range strings.Split(out, "\n") {

		// remove leading & trailing spaces
		line = strings.TrimSpace(line)

		// skip blank lines
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "#") {
			continue
		}

		match := reShowMap.FindStringSubmatch(line)
		if len(match) != 3 {
			return nil, fmt.Errorf("error parsing maps: %v", line)
		}

		output[match[1]] = match[2]
	}

	return output, nil
}

func (hap *HAProxy) ShowMaps() ([]MapResult, error) {
	out, err := hap.Command(cmdShowMaps)
	if err != nil {
		return nil, err
	}

	maps := make([]MapResult, 0)

	for _, line := range strings.Split(out, "\n") {

		// remove leading & trailing spaces
		line = strings.TrimSpace(line)

		// skip blank lines
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "#") {
			continue
		}

		match := reShowMaps.FindStringSubmatch(line)
		if len(match) != 5 {
			return nil, fmt.Errorf("error parsing maps: %v", line)
		}

		m := MapResult{
			File:        match[2],
			Description: match[3],
		}

		id, err := strconv.Atoi(match[1])
		if err != nil {
			return nil, fmt.Errorf("error parsing map id: %v", err)
		}
		m.ID = id

		for _, counter := range strings.Split(match[4], " ") {
			switch {
			case reShowMapsCurrVer.MatchString(counter):
				cmatch := reShowMapsCurrVer.FindStringSubmatch(counter)
				if len(cmatch) < 2 {
					return nil, fmt.Errorf("error parsing curr_ver: %v", counter)
				}
				n, err := strconv.Atoi(cmatch[1])
				if err != nil {
					return nil, fmt.Errorf("error parsing curr_ver: %v", err)
				}
				m.CurrentVersion = n
			case reShowMapsNextVer.MatchString(counter):
				cmatch := reShowMapsNextVer.FindStringSubmatch(counter)
				if len(cmatch) < 2 {
					return nil, fmt.Errorf("error parsing next_ver: %v", counter)
				}
				n, err := strconv.Atoi(cmatch[1])
				if err != nil {
					return nil, fmt.Errorf("error parsing next_ver: %v", err)
				}
				m.NextVersion = n
			case reShowMapsEntryCnt.MatchString(counter):
				cmatch := reShowMapsEntryCnt.FindStringSubmatch(counter)
				if len(cmatch) < 2 {
					return nil, fmt.Errorf("error parsing entry_cnt: %v", counter)
				}
				n, err := strconv.Atoi(cmatch[1])
				if err != nil {
					return nil, fmt.Errorf("error parsing entry_cnt: %v", err)
				}
				m.EntryCount = n
			}
		}
		maps = append(maps, m)
	}
	return maps, nil
}
