package executor

import (
	"reflect"

	"github.com/devicelab-dev/maestro-runner/pkg/flow"
)

// cloneForRun returns a copy of step that expansion can rewrite without
// touching the step the flow holds.
//
// ExpandStep rewrites ${...} in place. Steps inside repeat, retry and runFlow
// are the same objects on every pass, so the first pass's values were baked
// in: `inputText: "${output.domains[output.counter]}"` in a repeat typed the
// first domain every time although the counter advanced (duckduckgo/Android's
// autofill suite). Each execution now expands its own copy, and the flow keeps
// the ${...} template.
//
// Maps, slices of values and pointed-to structs (conditions, selectors) are
// copied too, since expansion rewrites them in place. Nested steps are not:
// each one is copied when it runs.
func cloneForRun(step flow.Step) flow.Step {
	v := reflect.ValueOf(step)
	if v.Kind() != reflect.Pointer || v.IsNil() || v.Elem().Kind() != reflect.Struct {
		return step
	}
	cp := reflect.New(v.Elem().Type())
	cp.Elem().Set(v.Elem())
	copyContainers(cp.Elem())
	clone, ok := cp.Interface().(flow.Step)
	if !ok {
		return step
	}
	return clone
}

var stepInterface = reflect.TypeOf((*flow.Step)(nil)).Elem()

// copyContainers gives a struct its own copies of the maps and value slices
// it holds, recursing into embedded and nested structs.
func copyContainers(v reflect.Value) {
	for i := 0; i < v.NumField(); i++ {
		f := v.Field(i)
		if !f.CanSet() {
			continue
		}
		switch f.Kind() {
		case reflect.Map:
			if f.IsNil() {
				continue
			}
			m := reflect.MakeMapWithSize(f.Type(), f.Len())
			for _, k := range f.MapKeys() {
				m.SetMapIndex(k, f.MapIndex(k))
			}
			f.Set(m)
		case reflect.Slice:
			if f.IsNil() || f.Type().Elem().Implements(stepInterface) {
				continue
			}
			s := reflect.MakeSlice(f.Type(), f.Len(), f.Len())
			reflect.Copy(s, f)
			f.Set(s)
		case reflect.Pointer:
			// A condition or selector held by pointer is rewritten in place
			// by expansion too (`when: true: ${output.attempt > 0}` inside
			// a retry was baked to "false" on the first attempt, #176).
			if f.IsNil() || f.Elem().Kind() != reflect.Struct || f.Type().Implements(stepInterface) {
				continue
			}
			p := reflect.New(f.Elem().Type())
			p.Elem().Set(f.Elem())
			copyContainers(p.Elem())
			f.Set(p)
		case reflect.Struct:
			copyContainers(f)
		}
	}
}
