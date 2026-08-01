package httpapi

func strPtrOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
