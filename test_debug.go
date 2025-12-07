package harmonydb

import (
	"fmt"
	"testing"
)

func TestDebugEncodeDecode(t *testing.T) {
	node := &Node{isLeaf: true}
	node.appendLeafCell([]byte("key1"), []byte("value1"))
	
	encoded, err := node.encodeLeaf()
	if err != nil {
		t.Fatal(err)
	}
	
	fmt.Printf("Encoded first 50 bytes: %v\n", encoded[:50])
	fmt.Printf("Encoded length: %d\n", len(encoded))
	
	decoded := &Node{}
	if err := decoded.decode(encoded); err != nil {
		t.Fatal(err)
	}
	
	fmt.Printf("Decoded offsetCount: %d\n", len(decoded.offsets))
	fmt.Printf("Decoded isLeaf: %v\n", decoded.isLeaf)
}
