package openai

import "net/http"

// ServeChatCompletions exposes the chat completion handler for internal products
// that already performed their own authentication.
func ServeChatCompletions(w http.ResponseWriter, r *http.Request) {
	handleChatCompletions(w, r)
}
