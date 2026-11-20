package backend

// uidDiff returns UIDs in current but not known (added) and in known but
// not current (vanished).
func uidDiff(known, current []uint32) (added, vanished []uint32) {
	have := make(map[uint32]bool, len(known))
	for _, u := range known {
		if u != 0 {
			have[u] = true
		}
	}
	seen := make(map[uint32]bool, len(current))
	for _, u := range current {
		if u == 0 || seen[u] {
			continue
		}
		seen[u] = true
		if !have[u] {
			added = append(added, u)
		}
	}
	for _, u := range known {
		if u != 0 && !seen[u] {
			vanished = append(vanished, u)
		}
	}
	return added, vanished
}

func uidSetOf(uids []uint32) map[uint32]bool {
	m := make(map[uint32]bool, len(uids))
	for _, u := range uids {
		if u != 0 {
			m[u] = true
		}
	}
	return m
}
