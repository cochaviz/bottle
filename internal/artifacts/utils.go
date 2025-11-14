package artifacts

import (
	"errors"
	"strings"
)

func PathFromURI(uri string) (string, error) {
	if !strings.HasPrefix(uri, "file://") {
		return "", errors.New("Not a file:// URI")
	}
	return strings.Split(uri, ":")[1], nil
}
