package bookmarks

// Navigator coordinates circular movement across workspace bookmarks.
type Navigator struct {
	store *Store
}

// NewNavigator constructs a circular navigator for the provided Store.
func NewNavigator(store *Store) *Navigator {
	return &Navigator{store: store}
}

// Navigator returns a circular navigator attached to this store.
func (s *Store) Navigator() *Navigator {
	return NewNavigator(s)
}

// NextBookmark navigates circularly to the next bookmark strictly after (curFile, curLine).
func (n *Navigator) NextBookmark(curFile string, curLine int) *Bookmark {
	if n == nil || n.store == nil {
		return nil
	}
	return n.store.NextBookmark(curFile, curLine)
}

// PrevBookmark navigates circularly to the previous bookmark strictly before (curFile, curLine).
func (n *Navigator) PrevBookmark(curFile string, curLine int) *Bookmark {
	if n == nil || n.store == nil {
		return nil
	}
	return n.store.PrevBookmark(curFile, curLine)
}

// NextBookmark finds the next bookmark strictly after (curFile, curLine).
// If (curFile, curLine) is past the last bookmark, circularly wraps around to the first bookmark.
func (s *Store) NextBookmark(curFile string, curLine int) *Bookmark {
	s.mu.RLock()
	defer s.mu.RUnlock()

	bms := s.getSortedBookmarksLocked()
	if len(bms) == 0 {
		return nil
	}
	if len(bms) == 1 {
		return bms[0]
	}

	normCurFile := s.normalizePath(curFile)

	// Locate first bookmark strictly greater than current file/line position
	for _, bm := range bms {
		if s.comparePosition(bm.FilePath, bm.LineNumber, normCurFile, curLine) > 0 {
			return bm
		}
	}

	// Circular wrap around to the first bookmark in workspace
	return bms[0]
}

// PrevBookmark finds the previous bookmark strictly before (curFile, curLine).
// If (curFile, curLine) is before the first bookmark, circularly wraps around to the last bookmark.
func (s *Store) PrevBookmark(curFile string, curLine int) *Bookmark {
	s.mu.RLock()
	defer s.mu.RUnlock()

	bms := s.getSortedBookmarksLocked()
	if len(bms) == 0 {
		return nil
	}
	if len(bms) == 1 {
		return bms[0]
	}

	normCurFile := s.normalizePath(curFile)

	// Scan backwards for the bookmark strictly preceding current position
	for i := len(bms) - 1; i >= 0; i-- {
		bm := bms[i]
		if s.comparePosition(bm.FilePath, bm.LineNumber, normCurFile, curLine) < 0 {
			return bm
		}
	}

	// Circular wrap around to the last bookmark in workspace
	return bms[len(bms)-1]
}
