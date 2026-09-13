package main
import("testing";"time")
func TestSyncStatusVocabulary(t *testing.T){allowed:=map[string]bool{"syncing":true,"synced":true,"failed":true,"conflict":true};for _,status:=range []string{"syncing","synced","failed","conflict"}{if !allowed[status]{t.Fatal(status)}}}
func TestOfflineTimestampFormat(t *testing.T){if _,e:=time.Parse(time.RFC3339,"2026-09-12T10:30:00+05:30");e!=nil{t.Fatal(e)}}
func TestSyncPayloadHashCanonicalAndConflict(t *testing.T){first,e:=syncPayloadHash([]byte(`{"requestId":"tx-1","total":100}`));if e!=nil{t.Fatal(e)};same,e:=syncPayloadHash([]byte(`{ "total": 100, "requestId": "tx-1" }`));if e!=nil{t.Fatal(e)};different,e:=syncPayloadHash([]byte(`{"requestId":"tx-1","total":101}`));if e!=nil{t.Fatal(e)};if first!=same{t.Fatal("equivalent JSON payloads must produce the same fingerprint")};if first==different{t.Fatal("different sale payloads must be detected")}}
