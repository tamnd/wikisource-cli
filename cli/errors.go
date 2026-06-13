package cli

import (
	"errors"

	"github.com/tamnd/wikisource-cli/wikisource"
)

func isNotFound(err error) bool {
	return errors.Is(err, wikisource.ErrNotFound)
}
