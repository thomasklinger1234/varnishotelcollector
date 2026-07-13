package varnishcachestatreceiver

import "testing"

func Test_extractBackendNameFromCounter(t *testing.T) {
	type args struct {
		name string
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "empty",
			args: args{name: ""},
			want: "",
		},
		{
			name: "reload_2019-08-29T100458.<name> (varnish 4.x)",
			args: args{name: "reload_2019-08-29T100458.<name>"},
			want: "<name>",
		},
		{
			name: "reload_20191014_091124_78599.<name> (varnish 6+)",
			args: args{name: "reload_20191014_091124_78599.<name>"},
			want: "<name>",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractBackendNameFromCounter(tt.args.name); got != tt.want {
				t.Errorf("extractBackendNameFromCounter() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_normalizeMetricName(t *testing.T) {
	type args struct {
		name string
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "default",
			args: args{name: "MGT.child.running"},
			want: "mgt_child_running",
		},
		{
			name: "vbe",
			args: args{name: "VBE.boot.dyn_default(127.0.0.1:8080).pipe_out"},
			want: "vbe_boot_dyn_default_127_0_0_1_8080_pipe_out",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeMetricName(tt.args.name); got != tt.want {
				t.Errorf("normalizeMetricName() = %v, want %v", got, tt.want)
			}
		})
	}
}
