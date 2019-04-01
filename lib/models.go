package lib

type Fetcher struct {
	Name      string
	Arguments []string
}

type Download struct {
	OutputPath string
	Fetcher    Fetcher
	Arguments  []string
}
