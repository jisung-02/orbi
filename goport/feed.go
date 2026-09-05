package main

// 인앱 알림 피드

type FeedItem struct {
	Ts    int64  `json:"ts"`
	Icon  string `json:"icon"`
	Title string `json:"title"`
	Body  string `json:"body"`
}

const maxFeedItems = 60

func feedList(st *AppState) []FeedItem {
	st.feedMu.Lock()
	defer st.feedMu.Unlock()
	out := make([]FeedItem, len(st.feed))
	copy(out, st.feed)
	return out
}

func feedPush(st *AppState, icon, title, body string) {
	item := FeedItem{Ts: nowMs(), Icon: icon, Title: title, Body: body}
	st.feedMu.Lock()
	st.feed = append([]FeedItem{item}, st.feed...)
	if len(st.feed) > maxFeedItems {
		st.feed = st.feed[:maxFeedItems]
	}
	st.feedMu.Unlock()
	st.events.Publish("feed-new", item)
}

func feedClear(st *AppState) []FeedItem {
	st.feedMu.Lock()
	st.feed = []FeedItem{}
	st.feedMu.Unlock()
	st.events.Publish("feed", []FeedItem{})
	return []FeedItem{}
}
