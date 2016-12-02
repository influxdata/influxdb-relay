package relay

import (
	"fmt"
	"log"
	"os"
	"strings"
)

const (
	LOG = "relay.log"
)

func format(prefix, suffix string) string {
	name := parse(suffix)

	path := prefix + "/db-" + name

	return path
}

func parse(str string) string {
	var temp string

	_, err := fmt.Sscanf(str, "http://%v", &temp)
	if err != nil {
		log.Println("error:", err)
	}

	var r []string
	r = strings.Split(temp, ":")

	return r[0]
}

func Mkdir(str string) error {
	err := os.MkdirAll(str, 0775)
	if err != nil && !os.IsExist(err) {
		return err
	}

	return nil
}
