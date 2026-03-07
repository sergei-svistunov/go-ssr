package form

import (
	"testing"
)

func TestInput_SetGetValue(t *testing.T) {
	var inp Input[string]
	if inp.IsNotNull() {
		t.Fatal("new input should be null")
	}
	inp.SetValue("hello")
	if !inp.IsNotNull() {
		t.Fatal("should be not null after SetValue")
	}
	if inp.GetValue() != "hello" {
		t.Fatalf("expected 'hello', got %q", inp.GetValue())
	}
}

func TestInput_Error(t *testing.T) {
	var inp Input[int]
	if inp.HasError() {
		t.Fatal("new input should not have error")
	}
	inp.SetError("bad value")
	if !inp.HasError() {
		t.Fatal("should have error after SetError")
	}
	if inp.GetError() != "bad value" {
		t.Fatalf("expected 'bad value', got %q", inp.GetError())
	}
}

func TestInputMultiple_SetGetValue(t *testing.T) {
	var inp InputMultiple[int]
	if inp.IsNotNull() {
		t.Fatal("new input should be null")
	}
	inp.SetValue(map[int]struct{}{1: {}, 2: {}})
	if !inp.IsNotNull() {
		t.Fatal("should be not null after SetValue")
	}
	v := inp.GetValue()
	if _, ok := v[1]; !ok {
		t.Fatal("expected key 1 in value")
	}
	if _, ok := v[2]; !ok {
		t.Fatal("expected key 2 in value")
	}
}

func TestSelect_SetGetOptions(t *testing.T) {
	var sel Select[uint8]
	opts := []SelectOptionElement[uint8]{
		SelectOption[uint8]{Value: 1, Label: "One"},
		SelectOption[uint8]{Value: 2, Label: "Two"},
	}
	sel.SetOptions(opts)
	got := sel.GetOptions()
	if len(got) != 2 {
		t.Fatalf("expected 2 options, got %d", len(got))
	}
}

func TestSelect_SetGetValue(t *testing.T) {
	var sel Select[int]
	if sel.IsNotNull() {
		t.Fatal("new select should be null")
	}
	sel.SetValue(42)
	if !sel.IsNotNull() {
		t.Fatal("should be not null after SetValue")
	}
	if sel.GetValue() != 42 {
		t.Fatalf("expected 42, got %d", sel.GetValue())
	}
}

func TestSelectMultiple_SetGetOptions(t *testing.T) {
	var sel SelectMultiple[string]
	opts := []SelectOptionElement[string]{
		SelectOption[string]{Value: "a", Label: "A"},
	}
	sel.SetOptions(opts)
	got := sel.GetOptions()
	if len(got) != 1 {
		t.Fatalf("expected 1 option, got %d", len(got))
	}
}

func TestSelectMultiple_SetGetValue(t *testing.T) {
	var sel SelectMultiple[string]
	if sel.IsNotNull() {
		t.Fatal("new select should be null")
	}
	sel.SetValue(map[string]struct{}{"a": {}, "b": {}})
	if !sel.IsNotNull() {
		t.Fatal("should be not null after SetValue")
	}
	v := sel.GetValue()
	if len(v) != 2 {
		t.Fatalf("expected 2 values, got %d", len(v))
	}
}

func TestTextarea_SetGetValue(t *testing.T) {
	var ta Textarea
	if ta.IsNotNull() {
		t.Fatal("new textarea should be null")
	}
	ta.SetValue("some text")
	if !ta.IsNotNull() {
		t.Fatal("should be not null after SetValue")
	}
	if ta.GetValue() != "some text" {
		t.Fatalf("expected 'some text', got %q", ta.GetValue())
	}
}

func TestFile_GetValue(t *testing.T) {
	var f File
	if f.IsNotNull() {
		t.Fatal("new file should be null")
	}
	if f.GetValue() != nil {
		t.Fatal("new file value should be nil")
	}
}

func TestFileMultiple_GetValue(t *testing.T) {
	var f FileMultiple
	if f.IsNotNull() {
		t.Fatal("new file multiple should be null")
	}
	if f.GetValue() != nil {
		t.Fatal("new file multiple value should be nil")
	}
}

func TestBaseFormValues_HasError_NoElements(t *testing.T) {
	var bfv BaseFormValues
	if bfv.HasError() {
		t.Fatal("empty form should not have error")
	}
	if bfv.IsValidated() {
		t.Fatal("new form should not be validated")
	}

	bfv.SetError("form error")
	if !bfv.HasError() {
		t.Fatal("should have error after SetError")
	}
	if bfv.GetError() != "form error" {
		t.Fatalf("expected 'form error', got %q", bfv.GetError())
	}
}

func TestBaseFormValues_HasError_WithElements(t *testing.T) {
	var inp Input[string]
	var bfv BaseFormValues
	bfv.SetElements([]Element{&inp})

	if bfv.HasError() {
		t.Fatal("should not have error when elements are clean")
	}

	inp.SetError("required")
	if !bfv.HasError() {
		t.Fatal("should have error when element has error")
	}
}

func TestBaseFormValues_MarkValidated(t *testing.T) {
	var bfv BaseFormValues
	if bfv.IsValidated() {
		t.Fatal("should not be validated initially")
	}
	bfv.MarkValidated()
	if !bfv.IsValidated() {
		t.Fatal("should be validated after MarkValidated")
	}
}

func TestParseValue_String(t *testing.T) {
	var v string
	var errStr string
	parseValue("hello", &v, &errStr)
	if v != "hello" {
		t.Fatalf("expected 'hello', got %q", v)
	}
	if errStr != "" {
		t.Fatalf("unexpected error: %s", errStr)
	}
}

func TestParseValue_Int(t *testing.T) {
	var v int
	var errStr string
	parseValue("42", &v, &errStr)
	if v != 42 {
		t.Fatalf("expected 42, got %d", v)
	}
	if errStr != "" {
		t.Fatalf("unexpected error: %s", errStr)
	}
}

func TestParseValue_Int_Invalid(t *testing.T) {
	var v int
	var errStr string
	parseValue("abc", &v, &errStr)
	if errStr == "" {
		t.Fatal("expected error for invalid int")
	}
}

func TestParseValue_Float64(t *testing.T) {
	var v float64
	var errStr string
	parseValue("3.14", &v, &errStr)
	if v != 3.14 {
		t.Fatalf("expected 3.14, got %f", v)
	}
}

func TestParseValue_Bool(t *testing.T) {
	var v bool
	var errStr string
	parseValue("true", &v, &errStr)
	if !v {
		t.Fatal("expected true")
	}
}

func TestParseValue_Bool_Invalid(t *testing.T) {
	var v bool
	var errStr string
	parseValue("notabool", &v, &errStr)
	if errStr == "" {
		t.Fatal("expected error for invalid bool")
	}
}

func TestParseValue_Uint8(t *testing.T) {
	var v uint8
	var errStr string
	parseValue("255", &v, &errStr)
	if v != 255 {
		t.Fatalf("expected 255, got %d", v)
	}
}

func TestParseValue_Uint8_Overflow(t *testing.T) {
	var v uint8
	var errStr string
	parseValue("256", &v, &errStr)
	if errStr == "" {
		t.Fatal("expected error for uint8 overflow")
	}
}

func TestSelectOption_WriteHtml(t *testing.T) {
	// Tested via integration in handler_test.go benchmarks;
	// basic smoke test here
	opt := SelectOption[int]{Value: 1, Label: "One"}
	var buf = &bytesBuffer{}
	err := opt.WriteHtml(buf, func(v int) bool { return v == 1 })
	if err != nil {
		t.Fatal(err)
	}
	s := buf.String()
	if s != `<option value="1" selected>One</option>` {
		t.Fatalf("unexpected HTML: %q", s)
	}
}

func TestSelectOption_WriteHtml_NotSelected(t *testing.T) {
	opt := SelectOption[int]{Value: 2, Label: "Two"}
	var buf = &bytesBuffer{}
	err := opt.WriteHtml(buf, func(v int) bool { return false })
	if err != nil {
		t.Fatal(err)
	}
	s := buf.String()
	if s != `<option value="2">Two</option>` {
		t.Fatalf("unexpected HTML: %q", s)
	}
}

func TestSelectOption_WriteHtml_Disabled(t *testing.T) {
	opt := SelectOption[int]{Value: 3, Label: "Three", Disabled: true}
	var buf = &bytesBuffer{}
	err := opt.WriteHtml(buf, func(v int) bool { return false })
	if err != nil {
		t.Fatal(err)
	}
	s := buf.String()
	if s != `<option value="3" disabled>Three</option>` {
		t.Fatalf("unexpected HTML: %q", s)
	}
}

func TestSelectOptionGroup_WriteHtml(t *testing.T) {
	grp := SelectOptionGroup[int]{
		Label: "Group",
		Options: []SelectOptionElement[int]{
			SelectOption[int]{Value: 1, Label: "One"},
		},
	}
	var buf = &bytesBuffer{}
	err := grp.WriteHtml(buf, func(v int) bool { return false })
	if err != nil {
		t.Fatal(err)
	}
	s := buf.String()
	expected := `<optgroup label="Group"><option value="1">One</option></optgroup>`
	if s != expected {
		t.Fatalf("expected %q, got %q", expected, s)
	}
}

func TestSelectOption_WriteHtml_EscapesLabel(t *testing.T) {
	opt := SelectOption[string]{Value: "x", Label: "<script>"}
	var buf = &bytesBuffer{}
	err := opt.WriteHtml(buf, func(v string) bool { return false })
	if err != nil {
		t.Fatal(err)
	}
	s := buf.String()
	if s != `<option value="x">&lt;script&gt;</option>` {
		t.Fatalf("label not escaped: %q", s)
	}
}

type bytesBuffer struct {
	data []byte
}

func (b *bytesBuffer) Write(p []byte) (int, error) {
	b.data = append(b.data, p...)
	return len(p), nil
}

func (b *bytesBuffer) String() string {
	return string(b.data)
}
