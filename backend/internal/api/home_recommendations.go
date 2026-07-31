package api

import (
	"context"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/video-site/backend/internal/catalog"
)

const (
	homeRecommendationSessionTTL  = 24 * time.Hour
	maxHomeRecommendationSessions = 256
	homeRecommendationLookupChunk = 32
)

// homeRecommendationSession owns one shuffled snapshot of the visible library.
// The cursor advances only after a successful request, so every snapshot entry
// is consumed before the session starts another random round. requestMu also
// serializes concurrent refreshes from the same login session.
type homeRecommendationSession struct {
	requestMu     sync.Mutex
	roundVideoIDs []string
	roundCursor   int
	lastAccess    time.Time
}

// nextHomeRecommendationBatch reads from the current shuffled library snapshot.
// When a request reaches the end of a round, it may continue into a new round to
// keep the home grid full. IDs already returned earlier in that same response are
// moved to the end of the new round, preventing duplicate cards within one grid
// without marking those IDs as consumed in the new round.
func (s *Server) nextHomeRecommendationBatch(
	ctx context.Context,
	session *homeRecommendationSession,
	count int,
) ([]*catalog.Video, []string, int, error) {
	roundVideoIDs := session.roundVideoIDs
	roundCursor := session.roundCursor
	items := make([]*catalog.Video, 0, count)
	selected := make(map[string]struct{}, count)

	for len(items) < count {
		eligibleEnd := len(roundVideoIDs)
		if roundCursor >= len(roundVideoIDs) {
			readyIDs, pendingIDs, err := s.Catalog.ListVisibleVideoIDsByThumbnailReadiness(ctx)
			if err != nil {
				return nil, nil, 0, err
			}
			rand.Shuffle(len(readyIDs), func(i, j int) {
				readyIDs[i], readyIDs[j] = readyIDs[j], readyIDs[i]
			})
			rand.Shuffle(len(pendingIDs), func(i, j int) {
				pendingIDs[i], pendingIDs[j] = pendingIDs[j], pendingIDs[i]
			})
			roundVideoIDs = append(readyIDs, pendingIDs...)
			roundCursor = 0
			eligibleEnd = len(roundVideoIDs)
			if len(roundVideoIDs) == 0 {
				break
			}

			if len(selected) > 0 {
				roundVideoIDs, eligibleEnd = postponeSelectedHomeVideoIDs(roundVideoIDs, selected)
				if eligibleEnd == 0 {
					// The library is smaller than the requested grid. Keep the freshly
					// created round intact for the next request instead of duplicating
					// cards in this response.
					break
				}
			}
		}

		loaded, nextCursor, err := s.loadHomeRecommendationRange(
			ctx,
			roundVideoIDs,
			roundCursor,
			eligibleEnd,
			count-len(items),
		)
		if err != nil {
			return nil, nil, 0, err
		}
		roundCursor = nextCursor
		for _, video := range loaded {
			if video == nil {
				continue
			}
			if _, exists := selected[video.ID]; exists {
				continue
			}
			selected[video.ID] = struct{}{}
			items = append(items, video)
		}

		if roundCursor < eligibleEnd {
			// The requested batch is full. loadHomeRecommendationRange only
			// stops before eligibleEnd when it has returned the requested count.
			break
		}
		if eligibleEnd < len(roundVideoIDs) {
			// The tail belongs to the newly-started round but was shown at the
			// end of the previous round in this response. Leave it for next time.
			break
		}
	}

	return items, roundVideoIDs, roundCursor, nil
}

func (s *Server) loadHomeRecommendationRange(
	ctx context.Context,
	videoIDs []string,
	cursor int,
	end int,
	count int,
) ([]*catalog.Video, int, error) {
	videos := make([]*catalog.Video, 0, count)
	for cursor < end && len(videos) < count {
		chunkEnd := cursor + homeRecommendationLookupChunk
		if chunkEnd > end {
			chunkEnd = end
		}
		visible, err := s.Catalog.VisibleVideosByIDs(ctx, videoIDs[cursor:chunkEnd])
		if err != nil {
			return nil, cursor, err
		}
		visibleByID := make(map[string]*catalog.Video, len(visible))
		for _, video := range visible {
			visibleByID[video.ID] = video
		}

		for cursor < chunkEnd && len(videos) < count {
			videoID := videoIDs[cursor]
			cursor++
			if video := visibleByID[videoID]; video != nil {
				videos = append(videos, video)
			}
		}
	}
	return videos, cursor, nil
}

func postponeSelectedHomeVideoIDs(videoIDs []string, selected map[string]struct{}) ([]string, int) {
	available := make([]string, 0, len(videoIDs))
	postponed := make([]string, 0, len(selected))
	for _, id := range videoIDs {
		if _, exists := selected[id]; exists {
			postponed = append(postponed, id)
			continue
		}
		available = append(available, id)
	}
	eligibleEnd := len(available)
	return append(available, postponed...), eligibleEnd
}

func (s *Server) homeRecommendationsNow() time.Time {
	if s.homeRecommendationNow != nil {
		return s.homeRecommendationNow()
	}
	return time.Now()
}

func (s *Server) homeRecommendationSession(identity string) *homeRecommendationSession {
	now := s.homeRecommendationsNow()
	s.homeRecommendationMu.Lock()
	defer s.homeRecommendationMu.Unlock()

	if s.homeRecommendationSessions == nil {
		s.homeRecommendationSessions = make(map[string]*homeRecommendationSession)
	}
	s.pruneHomeRecommendationSessionsLocked(now)
	if session := s.homeRecommendationSessions[identity]; session != nil {
		session.lastAccess = now
		return session
	}

	for len(s.homeRecommendationSessions) >= maxHomeRecommendationSessions {
		var oldestIdentity string
		var oldestAccess time.Time
		for candidateIdentity, session := range s.homeRecommendationSessions {
			if oldestIdentity == "" || session.lastAccess.Before(oldestAccess) {
				oldestIdentity = candidateIdentity
				oldestAccess = session.lastAccess
			}
		}
		if oldestIdentity == "" {
			break
		}
		delete(s.homeRecommendationSessions, oldestIdentity)
	}

	session := &homeRecommendationSession{lastAccess: now}
	s.homeRecommendationSessions[identity] = session
	return session
}

func (s *Server) touchHomeRecommendationSession(session *homeRecommendationSession) {
	s.homeRecommendationMu.Lock()
	defer s.homeRecommendationMu.Unlock()
	session.lastAccess = s.homeRecommendationsNow()
}

func (s *Server) pruneHomeRecommendationSessionsLocked(now time.Time) {
	for identity, session := range s.homeRecommendationSessions {
		if now.Sub(session.lastAccess) >= homeRecommendationSessionTTL {
			delete(s.homeRecommendationSessions, identity)
		}
	}
}
