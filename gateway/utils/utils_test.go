package utils

import (
	"testing"
)

func TestAcceptableIDType(t *testing.T) {
	type args struct {
		id interface{}
	}
	tests := []struct {
		name  string
		args  args
		want  string
		want1 bool
	}{
		// TODO: Add test cases.
		{
			name: "valid int",
			args: args{
				id: 5,
			},
			want:  "5",
			want1: true,
		},
		{
			name: "string",
			args: args{
				id: "SPACE-UP",
			},
			want:  "SPACE-UP",
			want1: true,
		},
		{
			name: "valid float",
			args: args{
				id: 5.0,
			},
			want:  "5",
			want1: true,
		},
		{
			name: "invalid float",
			args: args{
				id: 5.5,
			},

			want1: false,
		},
		{
			name: "valid int32",
			args: args{
				id: int32(5),
			},
			want:  "5",
			want1: true,
		},
		{
			name: "valid int32",
			args: args{
				id: int64(5),
			},
			want:  "5",
			want1: true,
		},
		{
			name: "dafault",
			args: args{
				id: true,
			},

			want1: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, got1 := AcceptableIDType(tt.args.id)
			if got != tt.want {
				t.Errorf("AcceptableIDType() got = %v, want %v", got, tt.want)
			}
			if got1 != tt.want1 {
				t.Errorf("AcceptableIDType() got1 = %v, want %v", got1, tt.want1)
			}
		})
	}
}

func TestGetIDVariable(t *testing.T) {
	type args struct {
		dbType string
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "mongo",
			args: args{
				dbType: "mongo",
			},
			want: "_id",
		},
		{
			name: "sql",
			args: args{
				dbType: "SQL",
			},
			want: "id",
		},
		{
			name: "invalid",
			args: args{
				dbType: "kdsf",
			},
			want: "id",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GetIDVariable(tt.args.dbType); got != tt.want {
				t.Errorf("GetIDVariable() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestArrayContains(t *testing.T) {
	type args struct {
		array []interface{}
		value interface{}
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{name: "valid string test case - found", args: args{value: "val2", array: []interface{}{"val1", "val2", "val3"}}, want: true},
		{name: "valid string test case - not found", args: args{value: "val", array: []interface{}{"val1", "val2", "val3"}}, want: false},
		{name: "valid string int case - fount", args: args{value: 2, array: []interface{}{1, 2, 3}}, want: true},
		{name: "valid string int case - not found", args: args{value: 20, array: []interface{}{1, 2, 3}}, want: false},
		{name: "passing array with multiple types - found", args: args{value: "2", array: []interface{}{1, "2", 3}}, want: true},
		{name: "passing array with multiple types - not found", args: args{value: 20, array: []interface{}{1, "2", 3}}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ArrayContains(tt.args.array, tt.args.value); got != tt.want {
				t.Errorf("ArrayContains() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsValidJoin(t *testing.T) {
	type args struct {
		on             map[string]interface{}
		jointTableName string
	}
	tests := []struct {
		name  string
		args  args
		want  bool
		want1 string
	}{
		{
			name: "joint table on the right",
			args: args{
				on:             map[string]interface{}{"user.id": "post.user_id"},
				jointTableName: "post",
			},
			want:  true,
			want1: "user_id",
		},
		{
			name: "joint table on the right with eq",
			args: args{
				on:             map[string]interface{}{"user.id": map[string]interface{}{"$eq": "post.user_id"}},
				jointTableName: "post",
			},
			want:  true,
			want1: "user_id",
		},
		{
			name: "joint table on the right with invalid operator",
			args: args{
				on:             map[string]interface{}{"user.id": map[string]interface{}{"$ne": "post.user_id"}},
				jointTableName: "post",
			},
			want:  false,
			want1: "none",
		},
		{
			name: "joint table on the right with literal",
			args: args{
				on:             map[string]interface{}{"user.id": map[string]interface{}{"$eq": 10}},
				jointTableName: "post",
			},
			want:  false,
			want1: "none",
		},
		{
			name: "joint table on the right with literal",
			args: args{
				on:             map[string]interface{}{"user.id": 10},
				jointTableName: "post",
			},
			want:  false,
			want1: "none",
		},
		{
			name: "joint table on the left",
			args: args{
				on:             map[string]interface{}{"post.user_id": "user.id"},
				jointTableName: "post",
			},
			want:  true,
			want1: "user_id",
		},
		{
			name: "joint table on the left with eq",
			args: args{
				on:             map[string]interface{}{"post.user_id": map[string]interface{}{"$eq": "user.id"}},
				jointTableName: "post",
			},
			want:  true,
			want1: "user_id",
		},
		{
			name: "joint table on the left with ne",
			args: args{
				on:             map[string]interface{}{"post.user_id": map[string]interface{}{"$ne": "user.id"}},
				jointTableName: "post",
			},
			want:  false,
			want1: "none",
		},
		{
			name: "or clause",
			args: args{
				on:             map[string]interface{}{"$or": map[string]interface{}{"$or": "user.id"}},
				jointTableName: "post",
			},
			want:  false,
			want1: "none",
		},
		{
			name: "no join table",
			args: args{
				on:             map[string]interface{}{"abc.user_id": "user.id"},
				jointTableName: "post",
			},
			want:  false,
			want1: "none",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, got1 := IsValidJoin(tt.args.on, tt.args.jointTableName)
			if got != tt.want {
				t.Errorf("IsValidJoin() got = %v, want %v", got, tt.want)
			}
			if got1 != tt.want1 {
				t.Errorf("IsValidJoin() got1 = %v, want %v", got1, tt.want1)
			}
		})
	}
}
