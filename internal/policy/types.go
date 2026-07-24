package policy

type Action uint8

const (
	Unknown Action = iota
	Direct
	Proxy
)

type Source string

const (
	SourceExplicitDomain Source = "explicit-domain"
	SourceExplicitIP     Source = "explicit-ip"
	SourceChinaDomain    Source = "china-domain"
	SourceChinaIP        Source = "china-ip"
	SourceDefault        Source = "default"
)

type Decision struct {
	Action Action
	Source Source
	Domain string
}
