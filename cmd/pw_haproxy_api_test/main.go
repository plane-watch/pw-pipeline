package main

import (
	"github.com/kr/pretty"
	"github.com/rs/zerolog/log"
	"plane.watch/lib/haproxy"
)

func main() {
	hap := haproxy.New("tcp", "127.0.0.1:9999", log.Logger)
	//err := hap.SetMap("virt@api_key_to_mlat_server", "81db73fe-b007-4a7b-ab60-6630a9115fdd", "be_runway_mlat")
	//if err != nil {
	//	panic(err)
	//}

	err := hap.ShutdownSession("0x7fce77ec8800")
	pretty.Println(err)
}
