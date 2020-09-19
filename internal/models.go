package internal

type Fetcher struct {
	Name      string
	Arguments []string
}

type Download struct {
	Name      string
	Fetcher   Fetcher
	Arguments []string
}
