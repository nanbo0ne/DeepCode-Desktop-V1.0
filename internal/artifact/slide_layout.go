package artifact

import (
	"fmt"
	"strings"
)

type slideLine struct {
	text            string
	y, height, size int
}

func slideLines(slide Slide) ([]slideLine, error) {
	wrap := func(text string, width int) []string {
		var lines []string
		text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\t", "    ")
		for _, line := range strings.Split(text, "\n") {
			lines = append(lines, wrapRunes(line, width)...)
		}
		return lines
	}
	title := wrap(slide.Title, 20)
	var body []string
	for _, bullet := range slide.Bullets {
		body = append(body, wrap(bullet, 30)...)
	}
	if len(title) > 2 || len(body) > 13 {
		return nil, fmt.Errorf("text exceeds bounded slide layout (2 title lines of 20 characters, 13 body lines of 30 characters); shorten the title or split bullets across slides")
	}
	var lines []slideLine
	for i, text := range title {
		lines = append(lines, slideLine{text: text, y: 600000 + i*450000, height: 450000, size: 2800})
	}
	for i, text := range body {
		lines = append(lines, slideLine{text: text, y: 1800000 + i*320000, height: 320000, size: 1800})
	}
	return lines, nil
}
