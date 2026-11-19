package agent

import (
	"context"
	"testing"

	"github.com/betternas/betternas/internal/sysexec"
)

func TestSmartAttributesAndSSDWear(t *testing.T) {
	d, f, _ := storageEnv(t)
	f.Responses[smartCall] = sysexec.Result{Stdout: []byte(`{"smart_status":{"passed":true},"temperature":{"current":30},"power_on_time":{"hours":5},"logical_block_size":512,
	 "ata_smart_attributes":{"table":[
	  {"id":5,"name":"Reallocated_Sector_Ct","value":100,"worst":100,"thresh":10,"when_failed":"","flags":{"prefailure":true},"raw":{"value":0,"string":"0"}},
	  {"id":177,"name":"Wear_Leveling_Count","value":93,"worst":93,"thresh":0,"when_failed":"","flags":{"prefailure":false},"raw":{"value":70,"string":"70"}},
	  {"id":194,"name":"Temperature_Celsius","value":70,"worst":60,"thresh":0,"when_failed":"In_the_past","flags":{},"raw":{"value":30,"string":"30 (Min/Max 20/45)"}},
	  {"id":241,"name":"Total_LBAs_Written","value":99,"worst":99,"thresh":0,"when_failed":"-","flags":{},"raw":{"value":2000000000,"string":"2000000000"}}]}}`)}
	s, err := Smart(context.Background(), d, "/dev/sda")
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Attributes) != 4 || !s.Attributes[0].Prefail || s.Attributes[0].Threshold != 10 || s.Attributes[2].Raw != "30 (Min/Max 20/45)" {
		t.Fatalf("%+v", s.Attributes)
	}
	if !s.Attributes[2].Failing || s.Attributes[3].Failing || s.Attributes[0].Failing {
		t.Fatalf("Failing falsch: %+v", s.Attributes)
	}
	if s.Wear == nil || s.Wear.PercentUsed == nil || *s.Wear.PercentUsed != 7 || s.Wear.BytesWritten != 2000000000*512 {
		t.Fatalf("%+v", s.Wear)
	}
}

func TestSmartHDDHasNoWear(t *testing.T) {
	d, f, _ := storageEnv(t)
	f.Responses[smartCall] = sysexec.Result{Stdout: []byte(`{"smart_status":{"passed":true},"ata_smart_attributes":{"table":[{"id":9,"name":"Power_On_Hours","value":90,"worst":90,"thresh":0,"raw":{"value":100}}]}}`)}
	s, err := Smart(context.Background(), d, "/dev/sda")
	if err != nil || s.Wear != nil || len(s.Attributes) != 1 || s.Attributes[0].Raw != "100" {
		t.Fatalf("%+v %v", s, err)
	}
}

func TestSmartNVMeHealth(t *testing.T) {
	d, f, _ := storageEnv(t)
	f.Responses[smartCall] = sysexec.Result{Stdout: []byte(`{"smart_status":{"passed":true},"temperature":{"current":40},
	 "nvme_smart_health_information_log":{"critical_warning":0,"available_spare":98,"available_spare_threshold":10,"percentage_used":3,
	  "data_units_read":1000,"data_units_written":2000,"power_cycles":12,"unsafe_shutdowns":2,"media_errors":0,"num_err_log_entries":5}}`)}
	s, err := Smart(context.Background(), d, "/dev/sda")
	if err != nil || s.Wear == nil {
		t.Fatalf("%+v %v", s, err)
	}
	if *s.Wear.PercentUsed != 3 || *s.Wear.SpareLeft != 98 || s.Wear.BytesWritten != 2000*512000 {
		t.Fatalf("%+v", s.Wear)
	}
	if len(s.Attributes) != 9 || s.Attributes[7].Failing {
		t.Fatalf("%+v", s.Attributes)
	}
}
