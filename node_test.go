package harmonydb

import (
	"bytes"
	"testing"
)

func TestLeafNodeEncodeDecodeRoundtrip(t *testing.T) {
	node := &Node{
		isLeaf:  true,
		offsets: []uint16{},
	}

	testCases := []struct {
		key []byte
		val []byte
	}{
		{[]byte("key1"), []byte("value1")},
		{[]byte("key2"), []byte("value2")},
		{[]byte("key3"), []byte("value3")},
		{[]byte("apple"), []byte("red fruit")},
		{[]byte("banana"), []byte("yellow fruit")},
	}

	for _, tc := range testCases {
		node.appendLeafCell(tc.key, tc.val)
	}

	t.Logf("Before encoding: node has %d offsets and %d leaf cells", len(node.offsets), len(node.leafCell))

	encoded, err := node.encodeLeaf()
	if err != nil {
		t.Fatalf("Failed to encode leaf node: %v", err)
	}

	if len(encoded) != pageSize {
		t.Fatalf("Encoded leaf node size is %d, expected %d", len(encoded), pageSize)
	}

	t.Logf("Encoded first 20 bytes: %v", encoded[:20])

	decodedNode := &Node{}
	if err := decodedNode.decode(encoded); err != nil {
		t.Fatalf("Failed to decode leaf node: %v", err)
	}

	t.Logf("After decoding: node has %d offsets and %d leaf cells", len(decodedNode.offsets), len(decodedNode.leafCell))

	if !decodedNode.isLeaf {
		t.Errorf("Decoded node is not a leaf node")
	}

	if len(decodedNode.offsets) != len(node.offsets) {
		t.Fatalf("Decoded node has %d offsets, expected %d", len(decodedNode.offsets), len(node.offsets))
	}

	if len(decodedNode.leafCell) != len(node.leafCell) {
		t.Fatalf("Decoded node has %d leaf cells, expected %d", len(decodedNode.leafCell), len(node.leafCell))
	}

	for i := range node.offsets {
		origIdx := node.offsets[i]
		decodedIdx := decodedNode.offsets[i]

		origCell := node.leafCell[origIdx]
		decodedCell := decodedNode.leafCell[decodedIdx]

		if origCell.keySize != decodedCell.keySize {
			t.Errorf("Cell %d: keySize mismatch, got %d, expected %d", i, decodedCell.keySize, origCell.keySize)
		}

		if origCell.valSize != decodedCell.valSize {
			t.Errorf("Cell %d: valSize mismatch, got %d, expected %d", i, decodedCell.valSize, origCell.valSize)
		}

		if !bytes.Equal(origCell.key, decodedCell.key) {
			t.Errorf("Cell %d: key mismatch, got %s, expected %s", i, decodedCell.key, origCell.key)
		}

		if !bytes.Equal(origCell.val, decodedCell.val) {
			t.Errorf("Cell %d: val mismatch, got %s, expected %s", i, decodedCell.val, origCell.val)
		}
	}
}

func TestInternalNodeEncodeDecodeRoundtrip(t *testing.T) {
	node := &Node{
		isLeaf:     false,
		fileOffset: 8192,
		offsets:    []uint16{},
	}

	testCases := []struct {
		key        []byte
		fileOffset uint64
	}{
		{[]byte("key1"), 4096},
		{[]byte("key2"), 8192},
		{[]byte("key3"), 12288},
		{[]byte("apple"), 16384},
		{[]byte("banana"), 20480},
	}

	for _, tc := range testCases {
		node.appendInternalCell(tc.fileOffset, tc.key)
	}

	encoded, err := node.encodeInternal()
	if err != nil {
		t.Fatalf("Failed to encode internal node: %v", err)
	}

	if len(encoded) != pageSize {
		t.Fatalf("Encoded internal node size is %d, expected %d", len(encoded), pageSize)
	}

	decodedNode := &Node{}
	if err := decodedNode.decode(encoded); err != nil {
		t.Fatalf("Failed to decode internal node: %v", err)
	}

	if decodedNode.isLeaf {
		t.Errorf("Decoded node is a leaf node, expected internal")
	}

	if decodedNode.fileOffset != node.fileOffset {
		t.Errorf("Decoded node fileOffset is %d, expected %d", decodedNode.fileOffset, node.fileOffset)
	}

	if len(decodedNode.offsets) != len(node.offsets) {
		t.Fatalf("Decoded node has %d offsets, expected %d", len(decodedNode.offsets), len(node.offsets))
	}

	if len(decodedNode.internalCell) != len(node.internalCell) {
		t.Fatalf("Decoded node has %d internal cells, expected %d", len(decodedNode.internalCell), len(node.internalCell))
	}

	for i := range node.offsets {
		origIdx := node.offsets[i]
		decodedIdx := decodedNode.offsets[i]

		origCell := node.internalCell[origIdx]
		decodedCell := decodedNode.internalCell[decodedIdx]

		if !bytes.Equal(origCell.key, decodedCell.key) {
			t.Errorf("Cell %d: key mismatch, got %s, expected %s", i, decodedCell.key, origCell.key)
		}

		if origCell.fileOffset != decodedCell.fileOffset {
			t.Errorf("Cell %d: fileOffset mismatch, got %d, expected %d", i, decodedCell.fileOffset, origCell.fileOffset)
		}
	}
}

func TestLeafNodeAppendCell(t *testing.T) {
	node := &Node{
		isLeaf:  true,
		offsets: []uint16{},
	}

	key1 := []byte("key1")
	val1 := []byte("value1")
	node.appendLeafCell(key1, val1)

	if len(node.offsets) != 1 {
		t.Fatalf("Expected 1 offset, got %d", len(node.offsets))
	}

	if len(node.leafCell) != 1 {
		t.Fatalf("Expected 1 leaf cell, got %d", len(node.leafCell))
	}

	cell := node.leafCell[node.offsets[0]]
	if !bytes.Equal(cell.key, key1) {
		t.Errorf("Key mismatch, got %s, expected %s", cell.key, key1)
	}

	if !bytes.Equal(cell.val, val1) {
		t.Errorf("Value mismatch, got %s, expected %s", cell.val, val1)
	}

	key2 := []byte("key2")
	val2 := []byte("value2")
	node.appendLeafCell(key2, val2)

	if len(node.offsets) != 2 {
		t.Fatalf("Expected 2 offsets, got %d", len(node.offsets))
	}

	if len(node.leafCell) != 2 {
		t.Fatalf("Expected 2 leaf cells, got %d", len(node.leafCell))
	}
}

func TestLeafNodeInsertCell(t *testing.T) {
	node := &Node{
		isLeaf:  true,
		offsets: []uint16{},
	}

	node.appendLeafCell([]byte("key1"), []byte("value1"))
	node.appendLeafCell([]byte("key3"), []byte("value3"))

	node.insertLeafCell(1, []byte("key2"), []byte("value2"))

	if len(node.offsets) != 3 {
		t.Fatalf("Expected 3 offsets, got %d", len(node.offsets))
	}

	if len(node.leafCell) != 3 {
		t.Fatalf("Expected 3 leaf cells, got %d", len(node.leafCell))
	}

	if !bytes.Equal(node.leafCell[node.offsets[0]].key, []byte("key1")) {
		t.Errorf("First key should be key1, got %s", node.leafCell[node.offsets[0]].key)
	}

	if !bytes.Equal(node.leafCell[node.offsets[1]].key, []byte("key2")) {
		t.Errorf("Second key should be key2, got %s", node.leafCell[node.offsets[1]].key)
	}

	if !bytes.Equal(node.leafCell[node.offsets[2]].key, []byte("key3")) {
		t.Errorf("Third key should be key3, got %s", node.leafCell[node.offsets[2]].key)
	}
}

func TestInternalNodeAppendCell(t *testing.T) {
	node := &Node{
		isLeaf:  false,
		offsets: []uint16{},
	}

	key1 := []byte("key1")
	offset1 := uint64(4096)
	node.appendInternalCell(offset1, key1)

	if len(node.offsets) != 1 {
		t.Fatalf("Expected 1 offset, got %d", len(node.offsets))
	}

	if len(node.internalCell) != 1 {
		t.Fatalf("Expected 1 internal cell, got %d", len(node.internalCell))
	}

	cell := node.internalCell[node.offsets[0]]
	if !bytes.Equal(cell.key, key1) {
		t.Errorf("Key mismatch, got %s, expected %s", cell.key, key1)
	}

	if cell.fileOffset != offset1 {
		t.Errorf("FileOffset mismatch, got %d, expected %d", cell.fileOffset, offset1)
	}

	key2 := []byte("key2")
	offset2 := uint64(8192)
	node.appendInternalCell(offset2, key2)

	if len(node.offsets) != 2 {
		t.Fatalf("Expected 2 offsets, got %d", len(node.offsets))
	}

	if len(node.internalCell) != 2 {
		t.Fatalf("Expected 2 internal cells, got %d", len(node.internalCell))
	}
}

func TestInternalNodeInsertCell(t *testing.T) {
	node := &Node{
		isLeaf:  false,
		offsets: []uint16{},
	}

	node.appendInternalCell(4096, []byte("key1"))
	node.appendInternalCell(12288, []byte("key3"))

	node.insertInternalCell(1, 8192, []byte("key2"))

	if len(node.offsets) != 3 {
		t.Fatalf("Expected 3 offsets, got %d", len(node.offsets))
	}

	if len(node.internalCell) != 3 {
		t.Fatalf("Expected 3 internal cells, got %d", len(node.internalCell))
	}

	if !bytes.Equal(node.internalCell[node.offsets[0]].key, []byte("key1")) {
		t.Errorf("First key should be key1, got %s", node.internalCell[node.offsets[0]].key)
	}

	if !bytes.Equal(node.internalCell[node.offsets[1]].key, []byte("key2")) {
		t.Errorf("Second key should be key2, got %s", node.internalCell[node.offsets[1]].key)
	}

	if !bytes.Equal(node.internalCell[node.offsets[2]].key, []byte("key3")) {
		t.Errorf("Third key should be key3, got %s", node.internalCell[node.offsets[2]].key)
	}
}

func TestFindInsPointForKey(t *testing.T) {
	node := &Node{
		isLeaf:  true,
		offsets: []uint16{},
	}

	node.appendLeafCell([]byte("apple"), []byte("red"))
	node.appendLeafCell([]byte("grape"), []byte("purple"))
	node.appendLeafCell([]byte("orange"), []byte("orange"))

	tests := []struct {
		key              []byte
		expectedPosition uint16
	}{
		{[]byte("banana"), 1},
		{[]byte("apple"), 0},
		{[]byte("grape"), 1},
		{[]byte("zebra"), 3},
		{[]byte("aardvark"), 0},
		{[]byte("mango"), 2},
	}

	for _, tc := range tests {
		pos := node.findInsPointForKey(tc.key)
		if pos != tc.expectedPosition {
			t.Errorf("findInsPointForKey(%s) = %d, expected %d", tc.key, pos, tc.expectedPosition)
		}
	}
}

func TestFindChildPage(t *testing.T) {
	node := &Node{
		isLeaf:  false,
		offsets: []uint16{},
	}

	node.appendInternalCell(4096, []byte("apple"))
	node.appendInternalCell(8192, []byte("grape"))
	node.appendInternalCell(12288, []byte("orange"))

	tests := []struct {
		key            []byte
		expectedOffset uint16
	}{
		{[]byte("banana"), 0},
		{[]byte("apple"), 0},
		{[]byte("grape"), 1},
		{[]byte("zebra"), 2},
		{[]byte("aardvark"), 0},
		{[]byte("mango"), 1},
		{[]byte("orange"), 2},
		{[]byte("peach"), 2},
	}

	for _, tc := range tests {
		offset := node.findChildPage(tc.key)
		if offset != tc.expectedOffset {
			t.Errorf("findChildPage(%s) = %d, expected %d", tc.key, offset, tc.expectedOffset)
		}
	}
}

func TestFindLeafCell(t *testing.T) {
	node := &Node{
		isLeaf:  true,
		offsets: []uint16{},
	}

	node.appendLeafCell([]byte("apple"), []byte("red"))
	node.appendLeafCell([]byte("banana"), []byte("yellow"))
	node.appendLeafCell([]byte("grape"), []byte("purple"))

	cell := node.findLeafCell([]byte("banana"))
	if cell == nil {
		t.Fatal("Expected to find cell for 'banana', got nil")
	}

	if !bytes.Equal(cell.key, []byte("banana")) {
		t.Errorf("Found cell key is %s, expected 'banana'", cell.key)
	}

	if !bytes.Equal(cell.val, []byte("yellow")) {
		t.Errorf("Found cell value is %s, expected 'yellow'", cell.val)
	}

	notFound := node.findLeafCell([]byte("notexist"))
	if notFound != nil {
		t.Errorf("Expected nil for non-existent key, got %v", notFound)
	}
}

func TestCellKey(t *testing.T) {
	leafNode := &Node{
		isLeaf:  true,
		offsets: []uint16{},
	}

	leafNode.appendLeafCell([]byte("key1"), []byte("value1"))
	leafNode.appendLeafCell([]byte("key2"), []byte("value2"))

	key := leafNode.cellKey(leafNode.offsets[0])
	if !bytes.Equal(key, []byte("key1")) {
		t.Errorf("cellKey for leaf node returned %s, expected 'key1'", key)
	}

	internalNode := &Node{
		isLeaf:  false,
		offsets: []uint16{},
	}

	internalNode.appendInternalCell(4096, []byte("key1"))
	internalNode.appendInternalCell(8192, []byte("key2"))

	key = internalNode.cellKey(internalNode.offsets[1])
	if !bytes.Equal(key, []byte("key2")) {
		t.Errorf("cellKey for internal node returned %s, expected 'key2'", key)
	}
}

func TestNodeIsFull(t *testing.T) {
	leafNode := &Node{
		isLeaf:  true,
		offsets: []uint16{},
	}

	for i := 0; i < maxLeafNodeCells; i++ {
		leafNode.appendLeafCell([]byte("key"), []byte("value"))
	}

	if !leafNode.isFull() {
		t.Error("Leaf node should be full after reaching maxLeafNodeCells")
	}

	internalNode := &Node{
		isLeaf:  false,
		offsets: []uint16{},
	}

	for i := 0; i <= maxInternalNodeCells; i++ {
		internalNode.appendInternalCell(uint64(i*4096), []byte("key"))
	}

	if !internalNode.isFull() {
		t.Error("Internal node should be full after exceeding maxInternalNodeCells")
	}
}

func TestEmptyLeafNodeEncodeDecode(t *testing.T) {
	node := &Node{
		isLeaf:  true,
		offsets: []uint16{},
	}

	encoded, err := node.encodeLeaf()
	if err != nil {
		t.Fatalf("Failed to encode empty leaf node: %v", err)
	}

	if len(encoded) != pageSize {
		t.Fatalf("Encoded empty leaf node size is %d, expected %d", len(encoded), pageSize)
	}

	decodedNode := &Node{}
	if err := decodedNode.decode(encoded); err != nil {
		t.Fatalf("Failed to decode empty leaf node: %v", err)
	}

	if !decodedNode.isLeaf {
		t.Errorf("Decoded node is not a leaf node")
	}

	if len(decodedNode.offsets) != 0 {
		t.Errorf("Decoded empty node has %d offsets, expected 0", len(decodedNode.offsets))
	}

	if len(decodedNode.leafCell) != 0 {
		t.Errorf("Decoded empty node has %d leaf cells, expected 0", len(decodedNode.leafCell))
	}
}

func TestEmptyInternalNodeEncodeDecode(t *testing.T) {
	node := &Node{
		isLeaf:     false,
		fileOffset: 4096,
		offsets:    []uint16{},
	}

	encoded, err := node.encodeInternal()
	if err != nil {
		t.Fatalf("Failed to encode empty internal node: %v", err)
	}

	if len(encoded) != pageSize {
		t.Fatalf("Encoded empty internal node size is %d, expected %d", len(encoded), pageSize)
	}

	decodedNode := &Node{}
	if err := decodedNode.decode(encoded); err != nil {
		t.Fatalf("Failed to decode empty internal node: %v", err)
	}

	if decodedNode.isLeaf {
		t.Errorf("Decoded node is a leaf node, expected internal")
	}

	if len(decodedNode.offsets) != 0 {
		t.Errorf("Decoded empty node has %d offsets, expected 0", len(decodedNode.offsets))
	}

	if len(decodedNode.internalCell) != 0 {
		t.Errorf("Decoded empty node has %d internal cells, expected 0", len(decodedNode.internalCell))
	}
}
