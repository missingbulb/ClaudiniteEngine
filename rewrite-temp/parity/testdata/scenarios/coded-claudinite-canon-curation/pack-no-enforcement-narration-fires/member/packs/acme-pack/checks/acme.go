package checks

func init() {
	checksdk.Register(checksdk.Check{
		ID:   "acme-go-check",
		Tags: []string{"world"},
	})
}
