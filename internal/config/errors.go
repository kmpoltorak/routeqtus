package config

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
)

type FieldError struct {
	Path       string `json:"path"`
	Message    string `json:"message"`
	RouteID    string `json:"route_id,omitempty" doc:"Valid route ID, when known; path remains a JSON Pointer."`
	UpstreamID string `json:"upstream_id,omitempty" doc:"Valid upstream ID, when known."`
	HandlerID  string `json:"handler_id,omitempty" doc:"Valid handler or binding ID, when known."`
}

func (e FieldError) Error() string {
	context := []string{}
	for _, item := range []struct{ key, value string }{{"route_id", e.RouteID}, {"upstream_id", e.UpstreamID}, {"handler_id", e.HandlerID}} {
		if item.value != "" {
			context = append(context, item.key+"="+item.value)
		}
	}
	if len(context) == 0 {
		return e.Path + ": " + e.Message
	}
	return e.Path + " (" + strings.Join(context, ", ") + "): " + e.Message
}
func Invalid(path, message string) error { return FieldError{Path: path, Message: message} }

// FieldErrors preserves every field error in joined or wrapped validation errors.
func FieldErrors(err error) []FieldError {
	switch e := err.(type) {
	case FieldError:
		return []FieldError{e}
	case interface{ Unwrap() []error }:
		var fields []FieldError
		for _, child := range e.Unwrap() {
			fields = append(fields, FieldErrors(child)...)
		}
		return fields
	case interface{ Unwrap() error }:
		return FieldErrors(e.Unwrap())
	default:
		return nil
	}
}

var validErrorID = regexp.MustCompile(IdentifierPattern)

func (c Config) WithErrorContext(err error) error {
	fields := FieldErrors(err)
	if len(fields) == 0 {
		return err
	}
	contextual := make([]error, 0, len(fields))
	for _, f := range fields {
		parts := strings.Split(strings.TrimPrefix(f.Path, "/"), "/")
		if len(parts) >= 2 {
			i, e := strconv.Atoi(parts[1])
			if e == nil && i >= 0 {
				switch parts[0] {
				case "upstreams":
					if i < len(c.Upstreams) {
						f.UpstreamID = c.Upstreams[i].ID
					}
				case "handlers":
					if i < len(c.Handlers) {
						f.HandlerID = c.Handlers[i].ID
					}
				case "global_handlers":
					if i < len(c.GlobalHandlers) {
						f.HandlerID = c.GlobalHandlers[i]
					}
				case "routes":
					if i < len(c.Routes) {
						r := c.Routes[i]
						f.RouteID = r.ID
						if len(parts) >= 3 && parts[2] == "upstream" {
							f.UpstreamID = r.Upstream
						}
						if len(parts) >= 4 && parts[2] == "handlers" {
							j, e := strconv.Atoi(parts[3])
							if e == nil && j >= 0 && j < len(r.Handlers) {
								f.HandlerID = r.Handlers[j]
							}
						}
					}
				}
			}
		}
		for _, id := range []*string{&f.RouteID, &f.UpstreamID, &f.HandlerID} {
			if !validErrorID.MatchString(*id) {
				*id = ""
			}
		}
		contextual = append(contextual, f)
	}
	return errors.Join(contextual...)
}
