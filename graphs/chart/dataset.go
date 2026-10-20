package chart

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
)

// DatasetList accepts either one ECharts dataset object or an array of them.
type DatasetList []DatasetOption

func (d *DatasetList) UnmarshalJSON(data []byte) error {
	data = trimJSONSpace(data)
	if len(data) == 0 || string(data) == "null" {
		*d = nil
		return nil
	}
	if data[0] == '[' {
		var list []DatasetOption
		if err := json.Unmarshal(data, &list); err != nil {
			return err
		}
		*d = list
		return nil
	}
	var one DatasetOption
	if err := json.Unmarshal(data, &one); err != nil {
		return err
	}
	*d = DatasetList{one}
	return nil
}

// DatasetRow is one source row. Array rows keep positional values; object
// rows keep named fields. The custom JSON decoder accepts both ECharts forms.
type DatasetRow struct {
	Values []any          `json:"-"`
	Fields map[string]any `json:"-"`
}

func (r *DatasetRow) UnmarshalJSON(data []byte) error {
	data = trimJSONSpace(data)
	if len(data) == 0 || string(data) == "null" {
		*r = DatasetRow{}
		return nil
	}
	switch data[0] {
	case '[':
		var values []any
		if err := json.Unmarshal(data, &values); err != nil {
			return err
		}
		r.Values = values
		r.Fields = nil
		return nil
	case '{':
		var fields map[string]any
		if err := json.Unmarshal(data, &fields); err != nil {
			return err
		}
		r.Fields = fields
		r.Values = nil
		return nil
	default:
		return fmt.Errorf("chart: dataset row must be an array or object")
	}
}

func (r DatasetRow) MarshalJSON() ([]byte, error) {
	if r.Fields != nil {
		return json.Marshal(r.Fields)
	}
	return json.Marshal(r.Values)
}

// Value resolves one dimension from this row. dimension may be a field name,
// a decimal index, or one of the names in dimensions. fields take precedence
// over positional lookup so object rows can use either encode form.
func (r DatasetRow) Value(dimension string, dimensions []string) (any, bool) {
	if r.Fields != nil {
		if value, ok := r.Fields[dimension]; ok {
			return value, true
		}
	}
	index := -1
	if parsed, err := strconv.Atoi(dimension); err == nil {
		index = parsed
	} else {
		for i, name := range dimensions {
			if name == dimension {
				index = i
				break
			}
		}
	}
	if r.Values != nil {
		if index >= 0 && index < len(r.Values) {
			return r.Values[index], true
		}
		return nil, false
	}
	if index >= 0 && index < len(dimensions) {
		value, ok := r.Fields[dimensions[index]]
		return value, ok
	}
	return nil, false
}

// DatasetTransform is a native, composable source transform. Transforms run
// in declaration order before dimensions are resolved for series encoding.
type DatasetTransform func([]DatasetRow) []DatasetRow

func (f DatasetTransform) Apply(rows []DatasetRow) []DatasetRow {
	if f == nil {
		return append([]DatasetRow(nil), rows...)
	}
	return f(rows)
}

// FilterDataset keeps rows accepted by keep.
func FilterDataset(keep func(DatasetRow) bool) DatasetTransform {
	return func(rows []DatasetRow) []DatasetRow {
		out := make([]DatasetRow, 0, len(rows))
		for _, row := range rows {
			if keep == nil || keep(row) {
				out = append(out, row)
			}
		}
		return out
	}
}

// MapDataset applies fn to each row. Returning the same row is cheap; a new
// row can be used to synthesize dimensions.
func MapDataset(fn func(DatasetRow) DatasetRow) DatasetTransform {
	return func(rows []DatasetRow) []DatasetRow {
		out := make([]DatasetRow, len(rows))
		for i, row := range rows {
			if fn != nil {
				out[i] = fn(row)
			} else {
				out[i] = row
			}
		}
		return out
	}
}

// SortDataset sorts by dimension using the row's numeric or string value.
// Numeric values sort before strings; missing values sort last.
func SortDataset(dimension string, descending bool) DatasetTransform {
	return func(rows []DatasetRow) []DatasetRow {
		out := append([]DatasetRow(nil), rows...)
		sort.SliceStable(out, func(i, j int) bool {
			a, aok := out[i].Value(dimension, nil)
			b, bok := out[j].Value(dimension, nil)
			if !aok || !bok {
				return aok && !bok
			}
			cmp := compareDatasetValues(a, b)
			if descending {
				return cmp > 0
			}
			return cmp < 0
		})
		return out
	}
}

// DatasetOption declares one source dataset. Dimensions may be omitted for
// object rows (sorted field names are used) or array rows (x,y,value,dim3...).
type DatasetOption struct {
	Source     []DatasetRow           `json:"source,omitempty"`
	Dimensions []string               `json:"dimensions,omitempty"`
	Transform  []DatasetTransform     `json:"-"`
	Transforms []DatasetTransformSpec `json:"transform,omitempty"`
}

// EncodeOption maps dataset dimensions onto a series' semantic channels.
// Values may be dimension names or decimal indexes.
type EncodeOption struct {
	X     DatasetDimension `json:"x,omitempty"`
	Y     DatasetDimension `json:"y,omitempty"`
	Value DatasetDimension `json:"value,omitempty"`
	Name  DatasetDimension `json:"name,omitempty"`
}

// DatasetDimension accepts an ECharts dimension name or numeric index.
type DatasetDimension string

func (d *DatasetDimension) UnmarshalJSON(data []byte) error {
	if text, ok := rawString(data); ok {
		*d = DatasetDimension(text)
		return nil
	}
	if number, ok := rawFloat(data); ok {
		*d = DatasetDimension(strconv.FormatFloat(number, 'f', -1, 64))
		return nil
	}
	return fmt.Errorf("chart: invalid dataset dimension %s", data)
}

// DatasetTransformSpec is a JSON-declarative source transform. Supported
// types are filter (comparison against config dimension/value) and sort.
type DatasetTransformSpec struct {
	Type   string                 `json:"type"`
	Config DatasetTransformConfig `json:"config,omitempty"`
}

// DatasetTransformConfig configures filter and sort transforms.
type DatasetTransformConfig struct {
	Dimension DatasetDimension `json:"dimension,omitempty"`
	GT        *float64         `json:"gt,omitempty"`
	GTE       *float64         `json:"gte,omitempty"`
	LT        *float64         `json:"lt,omitempty"`
	LTE       *float64         `json:"lte,omitempty"`
	EQ        *float64         `json:"eq,omitempty"`
	NE        *float64         `json:"ne,omitempty"`
	Value     any              `json:"value,omitempty"`
	Order     string           `json:"order,omitempty"`
}

func resolveDatasetOption(opt Option) Option {
	if len(opt.Dataset) == 0 || len(opt.Series) == 0 {
		return opt
	}
	out := opt
	out.Series = append([]SeriesOption(nil), opt.Series...)
	for i := range out.Series {
		series := &out.Series[i]
		if len(series.Data) > 0 || series.DatasetIndex < 0 || series.DatasetIndex >= len(opt.Dataset) {
			continue
		}
		dataset := opt.Dataset[series.DatasetIndex]
		rows, dimensions := dataset.resolvedRows()
		series.Data = datasetDataPoints(rows, dimensions, series.Encode)
	}
	return out
}

func (d DatasetOption) resolvedRows() ([]DatasetRow, []string) {
	rows := append([]DatasetRow(nil), d.Source...)
	dimensions := append([]string(nil), d.Dimensions...)
	if len(dimensions) == 0 {
		dimensions = inferDatasetDimensions(rows)
	}
	for _, transform := range d.Transform {
		if transform != nil {
			rows = transform.Apply(rows)
		}
	}
	for _, transform := range d.Transforms {
		rows = applyDatasetTransformSpec(rows, dimensions, transform)
	}
	if len(dimensions) == 0 {
		dimensions = inferDatasetDimensions(rows)
	}
	return rows, dimensions
}

func inferDatasetDimensions(rows []DatasetRow) []string {
	if len(rows) == 0 {
		return nil
	}
	if len(rows[0].Fields) > 0 {
		keys := make([]string, 0, len(rows[0].Fields))
		for key := range rows[0].Fields {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		return keys
	}
	count := 0
	for _, row := range rows {
		if len(row.Values) > count {
			count = len(row.Values)
		}
	}
	names := []string{"x", "y", "value"}
	out := make([]string, count)
	for i := range out {
		if i < len(names) {
			out[i] = names[i]
		} else {
			out[i] = fmt.Sprintf("dim%d", i+1)
		}
	}
	return out
}

func datasetDataPoints(rows []DatasetRow, dimensions []string, encode *EncodeOption) []DataPoint {
	if len(rows) == 0 {
		return nil
	}
	enc := EncodeOption{}
	if encode != nil {
		enc = *encode
	}
	out := make([]DataPoint, 0, len(rows))
	for _, row := range rows {
		point := DataPoint{}
		numbers := make([]float64, 0, len(row.Values))
		firstString := ""
		if row.Values != nil {
			for _, raw := range row.Values {
				if number, ok := datasetNumber(raw); ok {
					numbers = append(numbers, number)
				} else if text, ok := datasetString(raw); ok && firstString == "" {
					firstString = text
				}
			}
		} else {
			for _, dimension := range dimensions {
				raw, ok := row.Value(dimension, dimensions)
				if !ok {
					continue
				}
				if number, ok := datasetNumber(raw); ok {
					numbers = append(numbers, number)
				} else if text, ok := datasetString(raw); ok && firstString == "" {
					firstString = text
				}
			}
		}
		point.Values = numbers
		if len(numbers) > 0 {
			point.X = numbers[0]
			point.Y = numbers[0]
		}
		if len(numbers) > 1 {
			point.Y = numbers[1]
		}
		if enc.X != "" {
			if raw, ok := row.Value(string(enc.X), dimensions); ok {
				if number, ok := datasetNumber(raw); ok {
					point.X = number
				}
			}
		}
		if enc.Y != "" {
			if raw, ok := row.Value(string(enc.Y), dimensions); ok {
				if number, ok := datasetNumber(raw); ok {
					point.Y = number
				}
			}
		}
		if enc.Value != "" {
			if raw, ok := row.Value(string(enc.Value), dimensions); ok {
				if number, ok := datasetNumber(raw); ok {
					point.Y = number
				}
			}
		}
		if enc.Name != "" {
			if raw, ok := row.Value(string(enc.Name), dimensions); ok {
				point.Name, _ = datasetString(raw)
			}
		}
		if point.Name == "" {
			point.Name = firstString
		}
		out = append(out, point)
	}
	return out
}

func applyDatasetTransformSpec(rows []DatasetRow, dimensions []string, spec DatasetTransformSpec) []DatasetRow {
	switch spec.Type {
	case "filter":
		out := make([]DatasetRow, 0, len(rows))
		cfg := spec.Config
		for _, row := range rows {
			raw, ok := row.Value(string(cfg.Dimension), dimensions)
			if !ok {
				continue
			}
			if datasetFilterMatches(raw, cfg) {
				out = append(out, row)
			}
		}
		return out
	case "sort":
		out := append([]DatasetRow(nil), rows...)
		descending := spec.Config.Order == "desc" || spec.Config.Order == "descending"
		sort.SliceStable(out, func(i, j int) bool {
			a, aok := out[i].Value(string(spec.Config.Dimension), dimensions)
			b, bok := out[j].Value(string(spec.Config.Dimension), dimensions)
			if !aok || !bok {
				return aok && !bok
			}
			cmp := compareDatasetValues(a, b)
			if descending {
				return cmp > 0
			}
			return cmp < 0
		})
		return out
	default:
		return rows
	}
}

func datasetFilterMatches(raw any, cfg DatasetTransformConfig) bool {
	number, numeric := datasetNumber(raw)
	if numeric {
		switch {
		case cfg.GT != nil && !(number > *cfg.GT):
			return false
		case cfg.GTE != nil && !(number >= *cfg.GTE):
			return false
		case cfg.LT != nil && !(number < *cfg.LT):
			return false
		case cfg.LTE != nil && !(number <= *cfg.LTE):
			return false
		case cfg.EQ != nil && !(number == *cfg.EQ):
			return false
		case cfg.NE != nil && !(number != *cfg.NE):
			return false
		}
	}
	if cfg.Value != nil {
		if want, ok := datasetNumber(cfg.Value); ok && numeric {
			return number == want
		}
		text, _ := datasetString(raw)
		want, _ := datasetString(cfg.Value)
		return text == want
	}
	return true
}

func datasetNumber(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case json.Number:
		number, err := v.Float64()
		return number, err == nil
	case string:
		number, err := strconv.ParseFloat(v, 64)
		return number, err == nil
	default:
		return 0, false
	}
}

func datasetString(value any) (string, bool) {
	switch v := value.(type) {
	case string:
		return v, true
	case nil:
		return "", false
	default:
		return fmt.Sprint(v), true
	}
}

func compareDatasetValues(a, b any) int {
	an, aNumber := datasetNumber(a)
	bn, bNumber := datasetNumber(b)
	if aNumber && bNumber {
		switch {
		case an < bn:
			return -1
		case an > bn:
			return 1
		default:
			return 0
		}
	}
	if aNumber != bNumber {
		if aNumber {
			return -1
		}
		return 1
	}
	as, _ := datasetString(a)
	bs, _ := datasetString(b)
	switch {
	case as < bs:
		return -1
	case as > bs:
		return 1
	default:
		return 0
	}
}

func trimJSONSpace(data []byte) []byte {
	for len(data) > 0 && (data[0] == ' ' || data[0] == '\n' || data[0] == '\r' || data[0] == '\t') {
		data = data[1:]
	}
	for len(data) > 0 {
		last := data[len(data)-1]
		if last != ' ' && last != '\n' && last != '\r' && last != '\t' {
			break
		}
		data = data[:len(data)-1]
	}
	return data
}
