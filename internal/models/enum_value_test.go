package models

import "testing"

func TestStringEnumValueParity(t *testing.T) {
	check := func(t *testing.T, got any, err error, want string) {
		t.Helper()
		if err != nil || got != want {
			t.Fatalf("got %v, %v; want %q", got, err, want)
		}
	}
	v, err := ProviderGitHub.Value()
	check(t, v, err, "github")
	v2, err := CredentialProvider("github").Value()
	check(t, v2, err, "github")
	v3, err := TaskStatus(TaskStatusReview).Value()
	check(t, v3, err, string(TaskStatusReview))

	errs := map[string]error{}
	_, errs["IntegrationProvider"] = IntegrationProvider("bogus").Value()
	_, errs["CredentialProvider"] = CredentialProvider("bogus").Value()
	_, errs["TaskStatus"] = TaskStatus("bogus").Value()
	_, errs["AssigneeType"] = AssigneeType("bogus").Value()
	_, errs["AttemptStatus"] = AttemptStatus("bogus").Value()
	_, errs["WebhookStatus"] = WebhookStatus("bogus").Value()
	for name, e := range errs {
		want := "invalid " + name + ": \"bogus\""
		if e == nil || e.Error() != want {
			t.Errorf("%s: got %v want %q", name, e, want)
		}
	}
}
