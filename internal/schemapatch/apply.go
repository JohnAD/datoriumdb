package schemapatch

import (
	"fmt"

	"github.com/JohnAD/ojson"
)

// Apply applies every update operation in spec to currentSchemaBytes in
// memory and returns the resulting schema document, pretty-printed with
// 2-space indentation. It does not validate DatoriumDB-specific schema
// rules (callers should recompile and run those checks separately).
func Apply(currentSchemaBytes []byte, spec *UpdateSpec) ([]byte, error) {
	root, err := ojson.ReadBytesNoSchema(currentSchemaBytes)
	if err != nil {
		return nil, fmt.Errorf("invalid current schema JSON: %w", err)
	}
	if root.Kind() != ojson.KindObject {
		return nil, fmt.Errorf("current schema root must be kind object")
	}
	for i, op := range spec.Updates {
		if err := applyOp(root, op); err != nil {
			return nil, fmt.Errorf("update[%d] (%s %s): %w", i, op.Op, op.Path, err)
		}
	}
	return root.ToPrettyJSONBytes(2), nil
}

func applyOp(root ojson.JSONValue, op UpdateOp) error {
	switch op.Op {
	case "add", "import":
		return applyAdd(root, op)
	case "remove", "abandon":
		return applyRemove(root, op)
	case "replace":
		return applyReplace(root, op)
	case "move":
		return applyMove(root, op)
	case "copy":
		return applyCopy(root, op)
	case "convert":
		return applyConvert(root, op)
	default:
		return fmt.Errorf("unsupported op %q", op.Op)
	}
}

func applyAdd(root ojson.JSONValue, op UpdateOp) error {
	segments := splitPath(op.Path)
	if len(segments) == 0 {
		return fmt.Errorf("cannot add at document root")
	}
	leafName := segments[len(segments)-1]
	parentChildren, err := resolveParentChildrenArray(root, segments[:len(segments)-1])
	if err != nil {
		return err
	}
	if _, _, found := findChild(parentChildren, leafName); found {
		return fmt.Errorf("field %q already exists in schema", leafName)
	}
	if op.Schema == nil {
		return fmt.Errorf("schema object is required")
	}
	entry := buildSchemaEntry(leafName, op.Schema)
	parentChildren.Append(entry)
	return nil
}

func applyRemove(root ojson.JSONValue, op UpdateOp) error {
	segments := splitPath(op.Path)
	parentChildren, idx, _, err := resolveNode(root, segments)
	if err != nil {
		return err
	}
	if _, err := parentChildren.RemoveTry(idx); err != nil {
		return err
	}
	return nil
}

func applyReplace(root ojson.JSONValue, op UpdateOp) error {
	segments := splitPath(op.Path)
	_, _, _, err := resolveNode(root, segments)
	if err != nil {
		return err
	}
	// replace only changes document values, not schema structure.
	return nil
}

func applyMove(root ojson.JSONValue, op UpdateOp) error {
	fromSegments := splitPath(op.From)
	toSegments := splitPath(op.Path)
	if len(toSegments) == 0 {
		return fmt.Errorf("cannot move to document root")
	}
	parentChildrenFrom, idxFrom, node, err := resolveNode(root, fromSegments)
	if err != nil {
		return err
	}
	newName := toSegments[len(toSegments)-1]
	parentChildrenTo, err := resolveParentChildrenArray(root, toSegments[:len(toSegments)-1])
	if err != nil {
		return err
	}
	if _, _, found := findChild(parentChildrenTo, newName); found {
		return fmt.Errorf("field %q already exists in schema", newName)
	}
	if _, err := parentChildrenFrom.RemoveTry(idxFrom); err != nil {
		return err
	}
	node.Set("name", ojson.NewString(newName))
	parentChildrenTo.Append(node)
	return nil
}

func applyCopy(root ojson.JSONValue, op UpdateOp) error {
	fromSegments := splitPath(op.From)
	toSegments := splitPath(op.Path)
	if len(toSegments) == 0 {
		return fmt.Errorf("cannot copy to document root")
	}
	_, _, node, err := resolveNode(root, fromSegments)
	if err != nil {
		return err
	}
	newName := toSegments[len(toSegments)-1]
	parentChildrenTo, err := resolveParentChildrenArray(root, toSegments[:len(toSegments)-1])
	if err != nil {
		return err
	}
	if _, _, found := findChild(parentChildrenTo, newName); found {
		return fmt.Errorf("field %q already exists in schema", newName)
	}
	clone, err := ojson.ReadBytesNoSchema(node.ToJSONBytes())
	if err != nil {
		return err
	}
	clone.Set("name", ojson.NewString(newName))
	parentChildrenTo.Append(clone)
	return nil
}

func applyConvert(root ojson.JSONValue, op UpdateOp) error {
	segments := splitPath(op.Path)
	parentChildren, idx, node, err := resolveNode(root, segments)
	if err != nil {
		return err
	}
	if op.Schema == nil {
		return fmt.Errorf("schema object is required")
	}
	existingName := node.Get("name").String()
	entry := buildSchemaEntry(existingName, op.Schema)
	if _, err := parentChildren.RemoveTry(idx); err != nil {
		return err
	}
	if err := parentChildren.InsertAtTry(idx, entry); err != nil {
		return err
	}
	return nil
}

func buildSchemaEntry(name string, schema *omap) ojson.JSONValue {
	entry := ojson.NewObject()
	entry.Set("name", ojson.NewString(name))
	for _, k := range schema.Keys() {
		if k == "name" {
			continue
		}
		v, _ := schema.Get(k)
		entry.Set(k, toJSONValue(v))
	}
	return entry
}

// resolveNode walks segments from root, matching object children by "name",
// and returns the parent's children array, the matched index within it, and
// the matched node itself.
func resolveNode(root ojson.JSONValue, segments []string) (ojson.JSONValue, int, ojson.JSONValue, error) {
	if len(segments) == 0 {
		return ojson.NewVoid(), -1, ojson.NewVoid(), fmt.Errorf("path must not be the document root")
	}
	children := root.Get("children")
	var node ojson.JSONValue
	idx := -1
	for i, seg := range segments {
		if !children.IsArray() {
			return ojson.NewVoid(), -1, ojson.NewVoid(), fmt.Errorf("field %q not found", seg)
		}
		idx, node, _ = findChild(children, seg)
		if idx == -1 {
			return ojson.NewVoid(), -1, ojson.NewVoid(), fmt.Errorf("field %q not found", seg)
		}
		if i < len(segments)-1 {
			if node.Kind() != ojson.KindObject {
				return ojson.NewVoid(), -1, ojson.NewVoid(), fmt.Errorf("field %q is not an object; cannot descend", seg)
			}
			children = node.Get("children")
		}
	}
	return children, idx, node, nil
}

// resolveParentChildrenArray walks parentSegments from root and returns the
// children array of the final object, creating it if needed.
func resolveParentChildrenArray(root ojson.JSONValue, parentSegments []string) (ojson.JSONValue, error) {
	node := root
	for _, seg := range parentSegments {
		children := node.Get("children")
		if !children.IsArray() {
			return ojson.NewVoid(), fmt.Errorf("field %q not found", seg)
		}
		idx, child, _ := findChild(children, seg)
		if idx == -1 {
			return ojson.NewVoid(), fmt.Errorf("field %q not found", seg)
		}
		if child.Kind() != ojson.KindObject {
			return ojson.NewVoid(), fmt.Errorf("field %q is not an object; cannot descend", seg)
		}
		node = child
	}
	children := node.Get("children")
	if !children.IsArray() {
		children = ojson.NewArray()
		node.Set("children", children)
		children = node.Get("children")
	}
	return children, nil
}

func findChild(children ojson.JSONValue, name string) (int, ojson.JSONValue, bool) {
	if !children.IsArray() {
		return -1, ojson.NewVoid(), false
	}
	for i, item := range children.Items() {
		if item.Get("name").String() == name {
			return i, item, true
		}
	}
	return -1, ojson.NewVoid(), false
}
