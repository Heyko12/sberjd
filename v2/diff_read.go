package jd

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"strings"
)

// ReadDiffFile reads a file in native jd format.
func ReadDiffFile(filename string) (Diff, error) {
	bytes, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	return readDiff(string(bytes))
}

// ReadDiffString reads a string in native jd format.
func ReadDiffString(s string) (Diff, error) {
	return readDiff(s)
}

func readDiff(s string) (Diff, error) {
	diff := Diff{}
	diffLines := strings.Split(s, "\n")
	const (
		INIT   = iota
		META   = iota
		BEFORE = iota
		AT     = iota
		REMOVE = iota
		ADD    = iota
		AFTER  = iota
	)
	var de DiffElement
	var state = INIT
	for i, dl := range diffLines {
		if len(dl) == 0 {
			continue
		}
		header := dl[:1]
		// Validate state transition.
		var transitionErr error
		allow := func(s ...string) {
			for _, s := range s {
				if s == header {
					return
				}
			}
			transitionErr = fmt.Errorf("Unexpected %c. Expecting one of %v", dl[0], s)
		}
		switch state {
		case INIT:
			allow("^", "@")
		case META:
			allow("^", "@")
		case AT:
			allow("[", " ", "-", "+")
		case BEFORE:
			allow(" ", "-", "+")
		case REMOVE:
			allow("-", "+", " ", "]", "^", "@")
		case ADD:
			allow("+", " ", "]", "^", "@")
		case AFTER:
			allow(" ", "]", "^", "@")
		}
		if transitionErr != nil {
			return errorAt(i, transitionErr.Error())
		}
		// Process line.
		switch header {
		case "^":
			if state == ADD || state == REMOVE {
				// Save the previous diff element.
				err := checkDiffElement(de)
				if err != nil {
					return errorAt(i, err.Error())
				}
				diff = append(diff, de)
			}
			n, err := ReadJsonString(dl[1:])
			if err != nil {
				return errorAt(i, "Invalid Metadata. %v", err.Error())
			}
			m, err := readMetadata(n)
			if err != nil {
				return errorAt(i, "Invalid Metadata. %v", err.Error())
			}
			de.Metadata = de.Metadata.merge(m)
			state = META
		case "@":
			if state == ADD || state == REMOVE || state == AFTER {
				// Save the previous diff element.
				err := checkDiffElement(de)
				if err != nil {
					return errorAt(i, err.Error())
				}
				diff = append(diff, de)
			}
			p, err := ReadJsonString(dl[1:])
			if err != nil {
				return errorAt(i, "Invalid path. %v", err.Error())
			}
			path, err := NewPath(p)
			if err != nil {
				return errorAt(i, err.Error())
			}
			de.Path = path
			de.Before = []JsonNode{}
			de.Remove = []JsonNode{}
			de.Add = []JsonNode{}
			de.After = []JsonNode{}
			state = AT
		case "[":
			if state != AT {
				return errorAt(i, "Invalid context. [ must appear immediately after @")
			}
			de.Before = append(de.Before, voidNode{})
			state = BEFORE
		case "]":
			if state != REMOVE && state != ADD && state != AFTER {
				return errorAt(i, "Invalid context. ] must appear at the end of the context")
			}
			de.After = append(de.After, voidNode{})
			state = AFTER
		case " ":
			switch {
			case state == AT || state == BEFORE:
				// Accumulate before context
				b, err := ReadJsonString(dl[1:])
				if err != nil {
					return errorAt(i, "Invalid context. %v", err.Error())
				}
				de.Before = append(de.Before, b)
				state = BEFORE
			case state == ADD || state == REMOVE || state == AFTER:
				a, err := ReadJsonString(dl[1:])
				if err != nil {
					return errorAt(i, "Invalid context. %v", err.Error())
				}
				de.After = append(de.After, a)
				// Accumulate after context
				state = AFTER
			default:
				return errorAt(i, "Invalid context. Must preceed or follow + or -")
			}
		case "-":
			v, err := ReadJsonString(dl[1:])
			if err != nil {
				return errorAt(i, "Invalid value. %v", err.Error())
			}
			de.Remove = append(de.Remove, v)
			state = REMOVE
		case "+":
			v, err := ReadJsonString(dl[1:])
			if err != nil {
				return errorAt(i, "Invalid value. %v", err.Error())
			}
			de.Add = append(de.Add, v)
			state = ADD
		default:
			errorAt(i, "Unexpected %v.", dl[0])
		}
	}
	if state == META {
		// ^ is not a valid terminal state.
		return errorAt(len(diffLines), "Unexpected end of diff. Expecting ^ or @.")
	}
	if state == AT {
		// @ is not a valid terminal state.
		return errorAt(len(diffLines), "Unexpected end of diff. Expecting - or +.")
	}
	if state != INIT {
		// Save the last diff element.
		// Empty string diff is valid so state could be INIT
		err := checkDiffElement(de)
		if err != nil {
			return errorAt(len(diffLines), err.Error())
		}
		diff = append(diff, de)
	}
	return diff, nil
}

func checkDiffElement(de DiffElement) error {
	if len(de.Add) > 1 || len(de.Remove) > 1 {
		// Must be an array-based type
		if len(de.Path) == 0 {
			return fmt.Errorf("zero length path with multiple add or remove")
		}
		switch de.Path[len(de.Path)-1].(type) {
		case PathSet, PathSetKeys, PathMultiset, PathMultisetKeys, PathIndex:
			return nil
		default:
			return fmt.Errorf("multiple add or remove in object")
		}
	}
	return nil
}

func errorAt(lineZeroIndex int, err string, i ...interface{}) (Diff, error) {
	line := lineZeroIndex + 1
	e := fmt.Sprintf(err, i...)
	return nil, fmt.Errorf("invalid diff at line %v. %v", line, e)
}

// ReadPatchFile reads a JSON Patch (RFC 6902) from a file. It is subject
// to the same restrictions as ReadPatchString.
func ReadPatchFile(filename string) (Diff, error) {
	bytes, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	return ReadPatchString(string(bytes))
}

// ReadPatchString reads a JSON Patch (RFC 6902) from a
// string. ReadPatchString supports a subset of the specification and
// requires a sequence of "test", "remove", "add" operations which mimics
// the strict patching strategy of a native jd patch.
//
// For example:
//
//	[
//	  {"op":"test","path":"/foo","value":"bar"},
//	  {"op":"remove","path":"/foo","value":"bar"},
//	  {"op":"add","path":"/foo","value":"baz"}
//	]
func ReadPatchString(s string) (Diff, error) {
	var patch []patchElement
	err := json.Unmarshal([]byte(s), &patch)
	if err != nil {
		return nil, err
	}
	var diff Diff
	if len(patch) == 0 {
		return diff, nil
	}
	var element DiffElement
	for {
		if len(patch) == 0 {
			return diff, nil
		}
		element, patch, err = readPatchDiffElement(patch)
		if err != nil {
			return nil, err
		}
		diff = append(diff, element)
	}
}

// setPatchDiffElementContext detects before and/or after context and
// sets it on the diff element. It returns what remains of the
// patch. We expect exactly zero or one test before and zero or one
// test after, both array indices, followed by either a test and
// replacement or add operation, also array indices. This is
// ridiculously specific and strict, but mostly here just so we can
// automate round-trip testing of the JSON Patch format.
func setPatchDiffElementContext(patch []patchElement, d *DiffElement) ([]patchElement, error) {
	if len(patch) == 0 {
		return nil, fmt.Errorf("unexpected end of JSON Patch")
	}
	if len(patch) == 1 || patch[0].Op != "test" {
		// No before or after context.
		d.Before = []JsonNode{voidNode{}}
		d.After = []JsonNode{voidNode{}}
		return patch, nil
	}
	// To tell if this has context and a change or just lines of
	// change, we need to compare the indices.
	path, err := readPointer(patch[0].Path)
	if err != nil {
		return nil, err
	}
	if len(path) == 0 {
		return nil, fmt.Errorf("empty path: %q", patch[0].Path)
	}
	firstIndex, ok := path[len(path)-1].(PathIndex)
	if !ok {
		return nil, fmt.Errorf("expected path for array. got %q", patch[0].Path)
	}
	path, err = readPointer(patch[1].Path)
	if err != nil {
		return nil, err
	}
	if len(path) == 0 {
		return nil, fmt.Errorf("empty path: %q", patch[1].Path)
	}
	secondIndex, ok := path[len(path)-1].(PathIndex)
	if !ok {
		return nil, fmt.Errorf("expected path for array. got %q", patch[1].Path)
	}
	switch {
	case firstIndex == secondIndex && (patch[1].Op == "replace" || patch[1].Op == "remove"):
		// No before or after context.
		d.Before = []JsonNode{voidNode{}}
		d.After = []JsonNode{voidNode{}}
		return patch, nil
	case firstIndex == secondIndex && (patch[1].Op == "add"):
		// After context with add, which inserts before,
		// moving the rest of the array forward.
		d.Before = []JsonNode{voidNode{}}
		after, err := NewJsonNode(patch[0].Value)
		if err != nil {
			return nil, err
		}
		d.After = []JsonNode{after}
		return patch[1:], nil
	case firstIndex == secondIndex-1 && patch[1].Op == "add":
		// Before context with add, which inserts right after.
		before, err := NewJsonNode(patch[0].Value)
		if err != nil {
			return nil, err
		}
		d.Before = []JsonNode{before}
		d.After = []JsonNode{voidNode{}}
		return patch[1:], nil
	default:
		if len(patch) == 2 {
			// Something else. Let the rest of the read
			// functions point out the error.
			return patch, nil
		}
	}
	path, err = readPointer(patch[2].Path)
	if err != nil {
		return nil, err
	}
	if len(path) == 0 {
		return nil, fmt.Errorf("empty path: %q", patch[2].Path)
	}
	thirdIndex, ok := path[len(path)-1].(PathIndex)
	if !ok {
		return nil, fmt.Errorf("expected path for array. got %q", patch[2].Path)
	}
	switch {
	case (patch[2].Op == "test" || patch[2].Op == "add") && thirdIndex <= secondIndex:
		// Before and after context.
		before, err := NewJsonNode(patch[0].Value)
		if err != nil {
			return nil, err
		}
		d.Before = []JsonNode{before}
		after, err := NewJsonNode(patch[1].Value)
		if err != nil {
			return nil, err
		}
		d.After = []JsonNode{after}
		return patch[2:], nil
	case patch[1].Op == "test" && (patch[2].Op == "replace" || patch[2].Op == "remove") && firstIndex > secondIndex:
		// After context with replace / remove.
		d.Before = []JsonNode{voidNode{}}
		after, err := NewJsonNode(patch[0].Value)
		if err != nil {
			return nil, err
		}
		d.After = []JsonNode{after}
		return patch[1:], nil
	case patch[1].Op == "test" && (patch[2].Op == "replace" || patch[2].Op == "remove") && firstIndex < secondIndex:
		// Before context with replace / remove.
		before, err := NewJsonNode(patch[0].Value)
		if err != nil {
			return nil, err
		}
		d.Before = []JsonNode{before}
		d.After = []JsonNode{voidNode{}}
		return patch[1:], nil
	default:
		// Something else.
		return patch, nil
	}
}

func readPatchDiffElement(patch []patchElement) (DiffElement, []patchElement, error) {
	d := DiffElement{}
	if len(patch) == 0 {
		return d, nil, fmt.Errorf("unexpected end of JSON Patch")
	}
	p := patch[0]
	var err error
	// Maybe read before and after context
	if p.Op == "test" {
		patch, err = setPatchDiffElementContext(patch, &d)
	}
	switch p.Op {
	case "test":
		// Read path.
		d.Path, err = readPointer(p.Path)
		if err != nil {
			return d, nil, err
		}
		// Read value to test and remove.
		testValue, err := NewJsonNode(p.Value)
		if err != nil {
			return d, nil, err
		}
		d.Remove = []JsonNode{testValue}
		patch = patch[1:]
		// Validate test and remove are paired because jd remove is strict.
		if len(patch) == 0 || patch[0].Op != "remove" {
			return d, nil, fmt.Errorf("JSON Patch test op must be followed by a remove op")
		}
		if patch[0].Path != p.Path {
			return d, nil, fmt.Errorf("JSON Patch remove op must have the same path as test op")
		}
		removeValue, err := NewJsonNode(patch[0].Value)
		if err != nil {
			return d, nil, err
		}
		if !testValue.Equals(removeValue) {
			return d, nil, fmt.Errorf("JSON Patch remove op must have the same value as test op")
		}
		return d, patch[1:], nil
	case "add":
		d.Path, err = readPointer(p.Path)
		if err != nil {
			return d, nil, err
		}
		addValue, err := NewJsonNode(p.Value)
		if err != nil {
			return d, nil, err
		}
		d.Add = []JsonNode{addValue}
		return d, patch[1:], nil
	default:
		return d, nil, fmt.Errorf("invalid JSON Patch: must be test/remove or add ops")
	}
}

// ReadMergeFile reads a JSON Merge Patch (RFC 7386) from a file.
func ReadMergeFile(filename string) (Diff, error) {
	bytes, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	return ReadMergeString(string(bytes))
}

// ReadMergeString reads a JSON Merge Patch (RFC 7386) from a string.
func ReadMergeString(s string) (Diff, error) {
	n, err := ReadJsonString(s)
	if err != nil {
		return nil, err
	}
	d := Diff{}
	if n.Equals(jsonObject{}) {
		return d, nil
	}
	p, err := NewPath(jsonArray{})
	if err != nil {
		return nil, err
	}
	return readMergeInto(d, p, n), nil
}

func readMergeInto(d Diff, p Path, n JsonNode) Diff {
	switch n := n.(type) {
	case jsonObject:
		for k, v := range n {
			d = readMergeInto(d, append(p.clone(), PathKey(k)), v)
		}
		if len(n) == 0 {
			return append(d, DiffElement{
				Metadata: Metadata{
					Merge: true,
				},
				Path: p.clone(),
				Add:  []JsonNode{newJsonObject()},
			})
		}
	case voidNode:
		return d
	default:
		if isNull(n) {
			n = voidNode{}
		}
		return append(d, DiffElement{
			Metadata: Metadata{
				Merge: true,
			},
			Path: p.clone(),
			Add:  []JsonNode{n},
		})
	}
	return d
}
