package storage

import "testing"

const showOK = `Label: 'bnas-tresor'  uuid: 3a1b2c3d-0000-4000-8000-000000000001
	Total devices 2 FS bytes used 114688
	devid    1 size 1072693248 used 127926272 path /dev/sda1
	devid    2 size 1072693248 used 127926272 path /dev/sdb1

`

const showMissing = `Label: 'bnas-tresor'  uuid: 3a1b2c3d-0000-4000-8000-000000000001
	Total devices 3 FS bytes used 114688
	devid    1 size 1072693248 used 127926272 path /dev/sda1
	devid    3 size 1072693248 used 127926272 path /dev/sdc1
	*** Some devices missing

`

const statsOK = `[/dev/sda1].write_io_errs    0
[/dev/sda1].read_io_errs     3
[/dev/sda1].flush_io_errs    0
[/dev/sda1].corruption_errs  2
[/dev/sda1].generation_errs  0
[devid:2].write_io_errs    7
[devid:2].read_io_errs     0
[devid:2].flush_io_errs    0
[devid:2].corruption_errs  0
[devid:2].generation_errs  0
`

const usageOK = `Overall:
    Device size:		        2145386496
    Device allocated:		         255852544
    Device unallocated:		        1889533952
    Device missing:		                 0
    Used:			            229376
    Free (estimated):		        1017118720	(min: 1017118720)
    Free (statfs, df):		        1017118720
    Data ratio:			              2.00
    Metadata ratio:		              2.00

Data,RAID1: Size:536870912, Used:114688 (0.02%)
   /dev/sda1	536870912
   /dev/sdb1	536870912

Metadata,RAID1: Size:268435456, Used:114688 (0.04%)
   /dev/sda1	268435456
   /dev/sdb1	268435456
`

const scrubOK = `UUID:             3a1b2c3d-0000-4000-8000-000000000001
Scrub started:    Fri Nov 20 10:00:00 2026
Status:           finished
Duration:         0:00:01
	data_extents_scrubbed: 1
	read_errors: 0
	csum_errors: 2
	verify_errors: 1
	super_errors: 0
	uncorrectable_errors: 1
	unverified_errors: 0
	corrected_errors: 2
	last_physical: 1081344
`

func TestParseBtrfsShow(t *testing.T) {
	devs, missing := ParseBtrfsShow(showOK)
	if len(devs) != 2 || missing != 0 || devs[1].Path != "/dev/sdb1" || devs[0].SizeBytes != 1072693248 {
		t.Fatalf("%+v %d", devs, missing)
	}
	devs, missing = ParseBtrfsShow(showMissing)
	if len(devs) != 2 || missing != 1 {
		t.Fatalf("%+v %d", devs, missing)
	}
	// Explizit als MISSING gelistete Platte
	devs, missing = ParseBtrfsShow("\tTotal devices 2 FS bytes used 1\n\tdevid    1 size 10 used 1 path /dev/sda1\n\tdevid    2 size 0 used 0 path MISSING\n")
	if missing != 1 || !devs[1].Missing || devs[1].Path != "" {
		t.Fatalf("%+v %d", devs, missing)
	}
	if d, m := ParseBtrfsShow(""); len(d) != 0 || m != 0 {
		t.Fatal("leer")
	}
}

func TestParseBtrfsStats(t *testing.T) {
	s := ParseBtrfsStats(statsOK)
	if s["/dev/sda1"].Read != 3 || s["/dev/sda1"].Corruption != 2 || s["/dev/sda1"].Total() != 5 || s["devid:2"].Write != 7 || s["/dev/sdb1"].Total() != 0 || len(s) != 2 {
		t.Fatalf("%+v", s)
	}
}

func TestParseBtrfsUsage(t *testing.T) {
	u := ParseBtrfsUsage(usageOK)
	if u.DeviceBytes != 2145386496 || u.FreeBytes != 1017118720 || u.DataRatio != 2 || u.DataProfile != "RAID1" || u.MetadataProfile != "RAID1" {
		t.Fatalf("%+v", u)
	}
}

func TestParseBtrfsScrub(t *testing.T) {
	s := ParseBtrfsScrub(scrubOK)
	if s == nil || s.Result != "finished" || s.Running || s.Errors != 3 || s.Uncorrectable != 1 || s.Corrected != 2 || s.Duration != "0:00:01" {
		t.Fatalf("%+v", s)
	}
	if ParseBtrfsScrub("") != nil || ParseBtrfsScrub("scrub status for abc\n\tno stats available\n") != nil {
		t.Fatal("nie geprüft muss nil sein")
	}
	if r := ParseBtrfsScrub("Status: running\n"); r == nil || !r.Running {
		t.Fatalf("%+v", r)
	}
}

func TestAssembleBtrfsStatus(t *testing.T) {
	devs, missing := ParseBtrfsShow(showOK)
	st := AssembleBtrfsStatus(devs, missing, ParseBtrfsStats(statsOK), ParseBtrfsUsage(usageOK), true, nil)
	if st.Degraded || st.RawBytes != 2*1072693248 || st.UsableBytes != 1072693248 || st.FreeBytes != 1017118720 || st.DataProfile != "RAID1" {
		t.Fatalf("%+v", st)
	}
	// sda1: 5 Fehler per Pfad, sdb1: 7 Schreibfehler per devid:2
	if st.Devices[0].Errors.Total() != 5 || st.Devices[1].Errors.Write != 7 || st.Errors != 12 {
		t.Fatalf("%+v", st)
	}
	devs, missing = ParseBtrfsShow(showMissing)
	st = AssembleBtrfsStatus(devs, missing, nil, BtrfsUsage{}, false, nil)
	if !st.Degraded || st.Missing != 1 || st.Mounted || st.UsableBytes != 0 {
		t.Fatalf("%+v", st)
	}
}
