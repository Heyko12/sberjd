package jd

func diff(
	a, b JsonNode,
	p Path,
	options []Option,
	strategy patchStrategy,
) Diff {
	d := make(Diff, 0)
	if a.Equals(b) {
		return d
	}
	var de DiffElement
	switch strategy {
	case mergePatchStrategy:
		de = DiffElement{
			Metadata: Metadata{
				Version: 2,
				Merge:   true,
			},
			Path: p.clone(),
			Add:  jsonArray{b},
		}
	default:
		de = DiffElement{
			Metadata: Metadata{
				Version: 2,
			},
			Path:   p.clone(),
			Remove: nodeList(a),
			Add:    nodeList(b),
		}
	}
	return append(d, de)
}
