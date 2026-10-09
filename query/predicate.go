package query

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
)

func (p Predicate) MarshalJSON() ([]byte, error) {
	if len(p) == 0 {
		return []byte("null"), nil
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	if len(p) == 1 {
		return marshalPredicateNode(p[0])
	}
	return marshalPredicateNode(Logical(PredicateAnd, p...))
}

// Validate enforces the same structural and operand budgets for predicates
// assembled in Go as UnmarshalJSON enforces at the public JSON boundary.
func (p Predicate) Validate() error {
	if len(p) == 0 {
		return nil
	}
	budget := predicateBudget{}
	stack := map[uintptr]bool{}
	for _, node := range p {
		if err := validatePredicateNode(node, 1, &budget, stack); err != nil {
			return err
		}
	}
	return nil
}

// RootConjuncts returns the independently staged conjuncts represented by the
// public root. It keeps programmatic construction equivalent to JSON decoding,
// which normalizes a tagged root AND into the same top-level sequence.
func (p Predicate) RootConjuncts() []Filter {
	if len(p) == 1 && p[0].Kind == PredicateAnd {
		return p[0].Children
	}
	return p
}

// MapLeaves returns an independently owned predicate with fn applied to every
// leaf while preserving the caller's boolean structure.
func (p Predicate) MapLeaves(fn func(Filter) (Filter, error)) (Predicate, error) {
	if fn == nil {
		return nil, fmt.Errorf("predicate leaf mapper is required")
	}
	out := make(Predicate, len(p))
	for i := range p {
		mapped, err := mapPredicateNode(p[i], fn)
		if err != nil {
			return nil, err
		}
		out[i] = mapped
	}
	return out, nil
}

func mapPredicateNode(node Filter, fn func(Filter) (Filter, error)) (Filter, error) {
	if node.Kind == "" {
		if len(node.Children) != 0 {
			return Filter{}, fmt.Errorf("filter predicate cannot contain children")
		}
		return fn(Filter{Field: node.Field, Operator: node.Operator, Value: node.Value})
	}
	out := Filter{Kind: node.Kind, Relationship: node.Relationship, Children: make([]Filter, len(node.Children))}
	for i := range node.Children {
		child, err := mapPredicateNode(node.Children[i], fn)
		if err != nil {
			return Filter{}, err
		}
		out.Children[i] = child
	}
	return out, nil
}

// Leaves returns copies of the leaf predicates in stable traversal order.
func (p Predicate) Leaves() []Filter {
	leaves := make([]Filter, 0)
	var visit func(Filter)
	visit = func(node Filter) {
		if node.Kind == "" {
			leaves = append(leaves, Filter{Field: node.Field, Operator: node.Operator, Value: node.Value})
			return
		}
		for _, child := range node.Children {
			visit(child)
		}
	}
	for _, node := range p {
		visit(node)
	}
	return leaves
}

func validatePredicateNode(node Filter, depth int, budget *predicateBudget, stack map[uintptr]bool) error {
	if depth > 8 {
		return fmt.Errorf("predicate exceeds maximum depth")
	}
	budget.nodes++
	if budget.nodes > 128 {
		return fmt.Errorf("predicate exceeds maximum nodes")
	}
	if node.Kind == "" {
		if len(node.Children) != 0 {
			return fmt.Errorf("filter predicate cannot contain children")
		}
		budget.leaves++
		if budget.leaves > 64 {
			return fmt.Errorf("predicate exceeds maximum leaves")
		}
		count, scalarBytes, err := filterOperandSize(node.Value)
		if err != nil {
			return err
		}
		budget.values += count
		budget.scalarBytes += scalarBytes
		if budget.values > 256 || budget.scalarBytes > 64<<10 {
			return fmt.Errorf("predicate operand limit exceeded")
		}
		return nil
	}
	if node.Kind != PredicateAnd && node.Kind != PredicateOr && node.Kind != PredicateNot && node.Kind != PredicateExists {
		return fmt.Errorf("unsupported predicate kind")
	}
	if node.Field != "" || node.Operator != "" || node.Value != nil {
		return fmt.Errorf("logical predicate cannot contain filter payload")
	}
	if node.Kind == PredicateExists {
		if node.Relationship == "" || len(node.Children) != 1 {
			return fmt.Errorf("exists predicate requires a relationship and one where predicate")
		}
	} else if node.Relationship != "" {
		return fmt.Errorf("logical predicate cannot contain relationship payload")
	} else if node.Kind == PredicateNot {
		if len(node.Children) != 1 {
			return fmt.Errorf("not predicate requires exactly one child")
		}
	} else if len(node.Children) < 2 || len(node.Children) > 32 {
		return fmt.Errorf("and/or predicate requires 2 to 32 children")
	}
	if len(node.Children) > 0 {
		pointer := reflect.ValueOf(node.Children).Pointer()
		if stack[pointer] {
			return fmt.Errorf("predicate contains a cycle")
		}
		stack[pointer] = true
		defer delete(stack, pointer)
	}
	for _, child := range node.Children {
		if err := validatePredicateNode(child, depth+1, budget, stack); err != nil {
			return err
		}
	}
	return nil
}

func (p *Predicate) UnmarshalJSON(data []byte) error {
	if err := rejectDuplicateJSONKeys(data); err != nil {
		return err
	}
	trimmed := bytes.TrimSpace(data)
	if bytes.Equal(trimmed, []byte("null")) {
		return fmt.Errorf("filters must be omitted or contain a predicate node")
	}
	if len(trimmed) > 0 && trimmed[0] == '[' {
		return fmt.Errorf("filters must use the predicate object contract")
	}
	// Complete the structural and aggregate-size pass before Filter decoding
	// performs exact-number compatibility checks. This keeps resource bounds
	// authoritative even when an early leaf also contains an invalid number.
	if err := inspectPredicateNode(data, 1, &predicateBudget{}); err != nil {
		return err
	}
	node, err := unmarshalPredicateNode(data, 1, &predicateBudget{})
	if err != nil {
		return err
	}
	if node.Kind == PredicateAnd {
		*p = append(Predicate(nil), node.Children...)
	} else {
		*p = Predicate{node}
	}
	return nil
}

func marshalPredicateNode(node Filter) ([]byte, error) {
	if node.Kind == "" {
		return json.Marshal(struct {
			Kind   string `json:"kind"`
			Filter Filter `json:"filter"`
		}{Kind: "filter", Filter: Filter{Field: node.Field, Operator: node.Operator, Value: node.Value}})
	}
	if node.Kind == PredicateExists {
		if node.Relationship == "" || len(node.Children) != 1 {
			return nil, fmt.Errorf("exists predicate requires a relationship and one where predicate")
		}
		where, err := marshalPredicateNode(node.Children[0])
		if err != nil {
			return nil, err
		}
		return json.Marshal(struct {
			Kind         PredicateKind   `json:"kind"`
			Relationship string          `json:"relationship"`
			Where        json.RawMessage `json:"where"`
		}{node.Kind, node.Relationship, where})
	}
	if node.Kind == PredicateNot {
		if len(node.Children) != 1 {
			return nil, fmt.Errorf("not predicate requires exactly one child")
		}
		child, err := marshalPredicateNode(node.Children[0])
		if err != nil {
			return nil, err
		}
		return json.Marshal(struct {
			Kind  PredicateKind   `json:"kind"`
			Child json.RawMessage `json:"child"`
		}{node.Kind, child})
	}
	if node.Kind != PredicateAnd && node.Kind != PredicateOr {
		return nil, fmt.Errorf("unsupported predicate kind")
	}
	if len(node.Children) < 2 || len(node.Children) > 32 {
		return nil, fmt.Errorf("and/or predicate requires 2 to 32 children")
	}
	children := make([]json.RawMessage, len(node.Children))
	for i := range node.Children {
		b, err := marshalPredicateNode(node.Children[i])
		if err != nil {
			return nil, err
		}
		children[i] = b
	}
	return json.Marshal(struct {
		Kind     PredicateKind     `json:"kind"`
		Children []json.RawMessage `json:"children"`
	}{node.Kind, children})
}

type predicateBudget struct{ nodes, leaves, values, scalarBytes int }

func inspectPredicateNode(data []byte, depth int, budget *predicateBudget) error {
	if depth > 8 {
		return fmt.Errorf("predicate exceeds maximum depth")
	}
	budget.nodes++
	if budget.nodes > 128 {
		return fmt.Errorf("predicate exceeds maximum nodes")
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if len(raw) == 0 {
		return fmt.Errorf("predicate node must be an object")
	}
	var kind string
	kindBytes, ok := raw["kind"]
	if !ok || json.Unmarshal(kindBytes, &kind) != nil || kind == "" {
		return fmt.Errorf("predicate node requires kind")
	}
	allowed := map[string]bool{"kind": true}
	switch PredicateKind(kind) {
	case "filter":
		allowed["filter"] = true
		filterBytes, ok := raw["filter"]
		if !ok {
			return fmt.Errorf("filter node requires filter")
		}
		budget.leaves++
		if budget.leaves > 64 {
			return fmt.Errorf("predicate exceeds maximum leaves")
		}
		var filter struct {
			Field    json.RawMessage `json:"field"`
			Operator json.RawMessage `json:"operator"`
			Value    json.RawMessage `json:"value"`
		}
		if err := strictUnmarshal(filterBytes, &filter); err != nil {
			return err
		}
		count, scalarBytes, err := rawFilterOperandSize(filter.Value)
		if err != nil {
			return err
		}
		budget.values += count
		budget.scalarBytes += scalarBytes
		if budget.values > 256 || budget.scalarBytes > 64<<10 {
			return fmt.Errorf("predicate operand limit exceeded")
		}
	case PredicateAnd, PredicateOr:
		allowed["children"] = true
		var children []json.RawMessage
		childrenBytes, ok := raw["children"]
		if !ok || json.Unmarshal(childrenBytes, &children) != nil {
			return fmt.Errorf("and/or predicate requires children")
		}
		if len(children) < 2 || len(children) > 32 {
			return fmt.Errorf("and/or predicate requires 2 to 32 children")
		}
		for _, child := range children {
			if err := inspectPredicateNode(child, depth+1, budget); err != nil {
				return err
			}
		}
	case PredicateNot:
		allowed["child"] = true
		child, ok := raw["child"]
		if !ok {
			return fmt.Errorf("not predicate requires child")
		}
		if err := inspectPredicateNode(child, depth+1, budget); err != nil {
			return err
		}
	case PredicateExists:
		allowed["relationship"] = true
		allowed["where"] = true
		var relationship string
		if value, ok := raw["relationship"]; !ok || json.Unmarshal(value, &relationship) != nil || relationship == "" {
			return fmt.Errorf("exists predicate requires relationship")
		}
		where, ok := raw["where"]
		if !ok {
			return fmt.Errorf("exists predicate requires where")
		}
		if err := inspectPredicateNode(where, depth+1, budget); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported predicate kind")
	}
	for key := range raw {
		if !allowed[key] {
			return fmt.Errorf("unknown predicate field %q", key)
		}
	}
	return nil
}

func rawFilterOperandSize(raw json.RawMessage) (int, int, error) {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return 0, 0, nil
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return 0, 0, err
	}
	validScalar := func(value any) bool {
		switch value.(type) {
		case nil, string, json.Number, bool:
			return true
		default:
			return false
		}
	}
	if values, ok := value.([]any); ok {
		for _, item := range values {
			if !validScalar(item) {
				return 0, 0, fmt.Errorf("filter array values must be scalar")
			}
		}
	} else if !validScalar(value) {
		return 0, 0, fmt.Errorf("filter value must be a scalar or flat scalar array")
	}
	return filterOperandSize(value)
}

func unmarshalPredicateNode(data []byte, depth int, budget *predicateBudget) (Filter, error) {
	if depth > 8 {
		return Filter{}, fmt.Errorf("predicate exceeds maximum depth")
	}
	budget.nodes++
	if budget.nodes > 128 {
		return Filter{}, fmt.Errorf("predicate exceeds maximum nodes")
	}
	var raw map[string]json.RawMessage
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&raw); err != nil {
		return Filter{}, err
	}
	if len(raw) == 0 {
		return Filter{}, fmt.Errorf("predicate node must be an object")
	}
	var kind string
	b, ok := raw["kind"]
	if !ok || json.Unmarshal(b, &kind) != nil || kind == "" {
		return Filter{}, fmt.Errorf("predicate node requires kind")
	}
	allowed := map[string]bool{"kind": true}
	node := Filter{}
	switch PredicateKind(kind) {
	case "filter":
		allowed["filter"] = true
		b, ok := raw["filter"]
		if !ok {
			return Filter{}, fmt.Errorf("filter node requires filter")
		}
		budget.leaves++
		if budget.leaves > 64 {
			return Filter{}, fmt.Errorf("predicate exceeds maximum leaves")
		}
		if err := strictUnmarshal(b, &node); err != nil {
			return Filter{}, err
		}
		count, scalarBytes, err := filterOperandSize(node.Value)
		if err != nil {
			return Filter{}, err
		}
		budget.values += count
		budget.scalarBytes += scalarBytes
		if budget.values > 256 || budget.scalarBytes > 64<<10 {
			return Filter{}, fmt.Errorf("predicate operand limit exceeded")
		}
	case PredicateAnd, PredicateOr:
		node.Kind = PredicateKind(kind)
		allowed["children"] = true
		var children []json.RawMessage
		b, ok := raw["children"]
		if !ok || json.Unmarshal(b, &children) != nil {
			return Filter{}, fmt.Errorf("and/or predicate requires children")
		}
		if len(children) < 2 || len(children) > 32 {
			return Filter{}, fmt.Errorf("and/or predicate requires 2 to 32 children")
		}
		for _, child := range children {
			resolved, err := unmarshalPredicateNode(child, depth+1, budget)
			if err != nil {
				return Filter{}, err
			}
			node.Children = append(node.Children, resolved)
		}
	case PredicateNot:
		node.Kind = PredicateNot
		allowed["child"] = true
		b, ok := raw["child"]
		if !ok {
			return Filter{}, fmt.Errorf("not predicate requires child")
		}
		child, err := unmarshalPredicateNode(b, depth+1, budget)
		if err != nil {
			return Filter{}, err
		}
		node.Children = []Filter{child}
	case PredicateExists:
		node.Kind = PredicateExists
		allowed["relationship"] = true
		allowed["where"] = true
		b, ok := raw["relationship"]
		if !ok || json.Unmarshal(b, &node.Relationship) != nil || node.Relationship == "" {
			return Filter{}, fmt.Errorf("exists predicate requires relationship")
		}
		b, ok = raw["where"]
		if !ok {
			return Filter{}, fmt.Errorf("exists predicate requires where")
		}
		where, err := unmarshalPredicateNode(b, depth+1, budget)
		if err != nil {
			return Filter{}, err
		}
		node.Children = []Filter{where}
	default:
		return Filter{}, fmt.Errorf("unsupported predicate kind")
	}
	for key := range raw {
		if !allowed[key] {
			return Filter{}, fmt.Errorf("unknown predicate field %q", key)
		}
	}
	return node, nil
}

func strictUnmarshal(data []byte, out any) error {
	if err := rejectDuplicateJSONKeys(data); err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	return dec.Decode(out)
}

func rejectDuplicateJSONKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := consumeJSONValue(decoder); err != nil {
		return err
	}
	if _, err := decoder.Token(); err == nil {
		return fmt.Errorf("unexpected trailing JSON token")
	} else if err != io.EOF {
		return err
	}
	return nil
}

func consumeJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return fmt.Errorf("JSON object key must be a string")
			}
			if seen[key] {
				return fmt.Errorf("duplicate JSON field %q", key)
			}
			seen[key] = true
			if err := consumeJSONValue(decoder); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil || closing != json.Delim('}') {
			return fmt.Errorf("invalid JSON object")
		}
	case '[':
		for decoder.More() {
			if err := consumeJSONValue(decoder); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil || closing != json.Delim(']') {
			return fmt.Errorf("invalid JSON array")
		}
	default:
		return fmt.Errorf("invalid JSON delimiter")
	}
	return nil
}

func filterOperandSize(value any) (int, int, error) {
	if value == nil {
		return 0, 0, nil
	}
	items := []any{value}
	reflected := reflect.ValueOf(value)
	if reflected.IsValid() && (reflected.Kind() == reflect.Slice || reflected.Kind() == reflect.Array) {
		items = make([]any, reflected.Len())
		for i := range items {
			items[i] = reflected.Index(i).Interface()
		}
	}
	count, total := 0, 0
	for _, item := range items {
		if item == nil {
			continue
		}
		if !isPredicateScalar(item) {
			return 0, 0, fmt.Errorf("filter value must be a scalar or flat scalar array")
		}
		b, err := json.Marshal(item)
		if err != nil {
			return 0, 0, err
		}
		count++
		total += len(b)
	}
	return count, total, nil
}

func isPredicateScalar(value any) bool {
	switch value.(type) {
	case string, bool, json.Number,
		int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64,
		float32, float64:
		return true
	default:
		return false
	}
}
