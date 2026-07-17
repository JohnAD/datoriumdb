package ojson

import (
	"fmt"
	"strconv"
	"strings"
)

// Path describes a location inside a JSON document.
type Path struct {
	segments []pathSegment
}

type pathSegment struct {
	field *string
	index *int
}

// RootPath returns the root JSON path.
func RootPath() Path {
	return Path{}
}

// Field returns a child path for an object field.
func (p Path) Field(name string) Path {
	segments := make([]pathSegment, 0, len(p.segments)+1)
	segments = append(segments, p.segments...)
	segments = append(segments, pathSegment{field: &name})
	return Path{segments: segments}
}

// Index returns a child path for an array item.
func (p Path) Index(index int) Path {
	segments := make([]pathSegment, 0, len(p.segments)+1)
	segments = append(segments, p.segments...)
	segments = append(segments, pathSegment{index: &index})
	return Path{segments: segments}
}

func (p Path) String() string {
	if len(p.segments) == 0 {
		return ""
	}

	parts := make([]string, 0, len(p.segments))
	for _, segment := range p.segments {
		if segment.field != nil {
			parts = append(parts, quoteJSONString(*segment.field))
			continue
		}
		if segment.index != nil {
			parts = append(parts, strconv.Itoa(*segment.index))
		}
	}
	return strings.Join(parts, ".")
}

func (p Path) visible() string {
	if rendered := p.String(); rendered != "" {
		return rendered
	}
	return "$"
}

// OJSONError reports a conversion or validation failure at a JSON path.
type OJSONError struct {
	Path   Path
	Reason string
}

func (e OJSONError) Error() string {
	return fmt.Sprintf("%s: %s", e.Path.visible(), e.Reason)
}

func pathError(path Path, format string, args ...any) error {
	return OJSONError{
		Path:   path,
		Reason: fmt.Sprintf(format, args...),
	}
}
