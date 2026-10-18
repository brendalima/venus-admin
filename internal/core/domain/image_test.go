package domain

import (
	"errors"
	"testing"
)

type fakeSource struct{}

func (fakeSource) Bytes() ([]byte, error) { return nil, nil }

func TestPhotoEditValidate(t *testing.T) {
	two := []PhotoSource{fakeSource{}, fakeSource{}}
	many := make([]PhotoSlot, MaxImages+1)
	manyNew := make([]PhotoSource, MaxImages+1)
	for i := range many {
		many[i] = PhotoSlot{NewIndex: i}
		manyNew[i] = fakeSource{}
	}

	tests := []struct {
		name    string
		edit    PhotoEdit
		wantErr bool
	}{
		{name: "nothing", edit: PhotoEdit{}},
		{
			name: "kept, new and removed",
			edit: PhotoEdit{
				Order:   []PhotoSlot{{NewIndex: 1}, {ImageID: "a"}, {NewIndex: 0}},
				Removed: []string{"b"},
				New:     two,
			},
		},
		{name: "new photo not placed", edit: PhotoEdit{Order: []PhotoSlot{{NewIndex: 0}}, New: two}, wantErr: true},
		{name: "new photo placed twice", edit: PhotoEdit{Order: []PhotoSlot{{NewIndex: 0}, {NewIndex: 0}, {NewIndex: 1}}, New: two}, wantErr: true},
		{name: "index out of range", edit: PhotoEdit{Order: []PhotoSlot{{NewIndex: 2}}, New: two[:1]}, wantErr: true},
		{name: "existing photo twice", edit: PhotoEdit{Order: []PhotoSlot{{ImageID: "a"}, {ImageID: "a"}}}, wantErr: true},
		{name: "kept and removed", edit: PhotoEdit{Order: []PhotoSlot{{ImageID: "a"}}, Removed: []string{"a"}}, wantErr: true},
		{name: "too many", edit: PhotoEdit{Order: many, New: manyNew}, wantErr: true},
		{name: "append ignores order", edit: PhotoEdit{AppendNew: true, New: two}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.edit.Validate()
			if !tt.wantErr {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				return
			}
			var v *ValidationError
			if !errors.As(err, &v) || v.Field != "photos" || v.Message == "" {
				t.Errorf("err = %v, want a ValidationError on photos", err)
			}
		})
	}
}
