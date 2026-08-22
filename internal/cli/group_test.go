package cli

import "testing"

func TestGroupCommandsHaveNoIndependentMembershipAPI(t *testing.T) {
	t.Parallel()
	root, _ := newRootCommand()
	var groupFound bool
	for _, command := range root.Commands() {
		if command.Name() != "group" {
			continue
		}
		groupFound = true
		for _, child := range command.Commands() {
			if child.Name() == "member" {
				t.Fatal("obsolete group member command is still registered")
			}
		}
	}
	if !groupFound {
		t.Fatal("group command is not registered")
	}
}
