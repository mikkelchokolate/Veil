package api

type UniqueStringList struct {
	values []string
}

func NewUniqueStringList(values []string) UniqueStringList {
	return UniqueStringList{values: append([]string(nil), values...)}
}

func (l UniqueStringList) Append(value string) UniqueStringList {
	for _, existing := range l.values {
		if existing == value {
			return l
		}
	}
	l.values = append(append([]string(nil), l.values...), value)
	return l
}

func (l UniqueStringList) Values() []string {
	return append([]string(nil), l.values...)
}

func appendUnique(values []string, value string) []string {
	return NewUniqueStringList(values).Append(value).Values()
}
