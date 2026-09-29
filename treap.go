package faceted

import "math/rand"

// idTreap is a randomized binary search tree over DocID, giving
// O(log n) insert/delete and ordered traversal with offset/limit.
type idTreap struct {
	root *treapNode
}

type treapNode struct {
	key         DocID
	left, right *treapNode
	priority    uint64
	size        int
}

func newIDTreap() *idTreap {
	return &idTreap{}
}

func (t *idTreap) add(id DocID) {
	if t.contains(id) {
		return
	}
	left, right := t.split(t.root, id)
	node := &treapNode{key: id, priority: rand.Uint64(), size: 1}
	t.root = t.merge(t.merge(left, node), right)
}

func (t *idTreap) remove(id DocID) {
	t.root = t.removeNode(t.root, id)
}

func (t *idTreap) removeNode(node *treapNode, id DocID) *treapNode {
	if node == nil {
		return nil
	}
	switch {
	case id < node.key:
		node.left = t.removeNode(node.left, id)
	case id > node.key:
		node.right = t.removeNode(node.right, id)
	default:
		node = t.merge(node.left, node.right)
	}
	if node != nil {
		node.updateSize()
	}
	return node
}

func (t *idTreap) contains(id DocID) bool {
	node := t.root
	for node != nil {
		switch {
		case id < node.key:
			node = node.left
		case id > node.key:
			node = node.right
		default:
			return true
		}
	}
	return false
}

func (t *idTreap) len() int {
	return nodeSize(t.root)
}

// page returns up to limit ids in ascending order, skipping the first
// offset members.
func (t *idTreap) page(offset, limit int) []DocID {
	if limit <= 0 || offset >= nodeSize(t.root) {
		return nil
	}
	out := make([]DocID, 0, limit)
	collectPage(t.root, offset, limit, &out)
	return out
}

// split partitions a tree into keys < key and keys >= key.
func (t *idTreap) split(node *treapNode, key DocID) (*treapNode, *treapNode) {
	if node == nil {
		return nil, nil
	}
	if node.key < key {
		left, right := t.split(node.right, key)
		node.right = left
		node.updateSize()
		return node, right
	}
	left, right := t.split(node.left, key)
	node.left = right
	node.updateSize()
	return left, node
}

func (t *idTreap) merge(left, right *treapNode) *treapNode {
	if left == nil {
		return right
	}
	if right == nil {
		return left
	}
	if left.priority > right.priority {
		left.right = t.merge(left.right, right)
		left.updateSize()
		return left
	}
	right.left = t.merge(left, right.left)
	right.updateSize()
	return right
}

func collectPage(node *treapNode, skip, limit int, out *[]DocID) {
	if node == nil || len(*out) >= limit {
		return
	}
	leftSize := nodeSize(node.left)
	switch {
	case skip >= leftSize+1:
		collectPage(node.right, skip-leftSize-1, limit, out)
	case skip == leftSize:
		*out = append(*out, node.key)
		collectPage(node.right, 0, limit, out)
	default:
		collectPage(node.left, skip, limit, out)
		if len(*out) < limit {
			*out = append(*out, node.key)
			collectPage(node.right, 0, limit, out)
		}
	}
}

func nodeSize(node *treapNode) int {
	if node == nil {
		return 0
	}
	return node.size
}

func (n *treapNode) updateSize() {
	n.size = 1 + nodeSize(n.left) + nodeSize(n.right)
}

// inorder walks keys in ascending order, skipping the first skip accepted
// keys and appending accepted keys to out until out reaches want entries.
// A nil keep accepts every key.
func inorder(node *treapNode, keep func(DocID) bool, skip, want int, out []DocID) ([]DocID, int) {
	if node == nil || len(out) >= want {
		return out, skip
	}
	out, skip = inorder(node.left, keep, skip, want, out)
	if len(out) < want {
		if keep == nil || keep(node.key) {
			if skip == 0 {
				out = append(out, node.key)
			} else {
				skip--
			}
		}
	}
	out, skip = inorder(node.right, keep, skip, want, out)
	return out, skip
}

// unionTreaps returns a new treap containing keys from both inputs.
// It iterates the smaller tree and inserts into the larger one, so cost
// tracks the size of the smaller operand (selected posting lists here).
func unionTreaps(a, b *idTreap) *idTreap {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	if a.len() < b.len() {
		a, b = b, a
	}
	out := &idTreap{root: cloneTreap(a.root)}
	insertAll(&out.root, b.root)
	return out
}

// intersectTreaps keeps only keys also present in keep. It builds a fresh
// treap so callers may mutate or share cached inputs freely.
func intersectTreaps(in *idTreap, keep *idTreap) *idTreap {
	if in == nil || keep == nil {
		return newIDTreap()
	}
	out := newIDTreap()
	var walk func(node *treapNode)
	walk = func(node *treapNode) {
		if node == nil {
			return
		}
		if keep.contains(node.key) {
			out.add(node.key)
		}
		walk(node.left)
		walk(node.right)
	}
	walk(in.root)
	return out
}

func cloneTreap(node *treapNode) *treapNode {
	if node == nil {
		return nil
	}
	return &treapNode{
		key:      node.key,
		priority: node.priority,
		size:     node.size,
		left:     cloneTreap(node.left),
		right:    cloneTreap(node.right),
	}
}

func insertAll(root **treapNode, node *treapNode) {
	if node == nil {
		return
	}
	*root = insertTreap(*root, node.key, node.priority)
	insertAll(root, node.left)
	insertAll(root, node.right)
}

func insertTreap(node *treapNode, key DocID, priority uint64) *treapNode {
	if node == nil {
		return &treapNode{key: key, priority: priority, size: 1}
	}
	if key == node.key {
		return node
	}
	if key < node.key {
		node.left = insertTreap(node.left, key, priority)
		if node.left.priority > node.priority {
			node = rotateRight(node)
		}
	} else {
		node.right = insertTreap(node.right, key, priority)
		if node.right.priority > node.priority {
			node = rotateLeft(node)
		}
	}
	node.updateSize()
	return node
}

func rotateRight(node *treapNode) *treapNode {
	left := node.left
	node.left = left.right
	left.right = node
	node.updateSize()
	left.updateSize()
	return left
}

func rotateLeft(node *treapNode) *treapNode {
	right := node.right
	node.right = right.left
	right.left = node
	node.updateSize()
	right.updateSize()
	return right
}
