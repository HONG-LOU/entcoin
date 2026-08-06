package main

type EntPayLinkStatus struct {
	Registered bool   `json:"registered"`
	Owner      string `json:"owner,omitempty"`
	Portable   bool   `json:"portable"`
	Message    string `json:"message"`
}

func (a *App) GetEntPayLinkStatus() (EntPayLinkStatus, error) {
	return getEntPayLinkStatus()
}

func (a *App) RegisterEntPayLinks() (EntPayLinkStatus, error) {
	return registerEntPayLinks()
}
