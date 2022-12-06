package stringhelper

func Deduplicate(strings []string) []string {
	seen := map[string]struct{}{}
	ret := make([]string, 0, len(strings))
	for _, s := range strings {
		if _, exists := seen[s]; exists {
			continue
		}
		seen[s] = struct{}{}
		ret = append(ret, s)
	}
	return ret
}
func RemoveEmptyAndDeDup(split []string) []string {
	ret := make([]string, 0, len(split))
	for _, s := range split {
		if s != "" {
			ret = append(ret, s)
		}
	}
	return Deduplicate(ret)
}
