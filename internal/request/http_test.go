package request_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"net/http"
	"reflect"
	"regexp"
	"sort"
	"testing"

	authenticationv1 "k8s.io/api/authentication/v1"
	authorizationv1 "k8s.io/api/authorization/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/projectcapsule/capsule-proxy/internal/request"
)

type testClient func(ctx context.Context, obj client.Object) error

func (t testClient) Delete(ctx context.Context, obj client.Object, _ ...client.DeleteOption) error {
	return t(ctx, obj)
}

func (t testClient) Update(ctx context.Context, obj client.Object, _ ...client.UpdateOption) error {
	return t(ctx, obj)
}

func (t testClient) Patch(ctx context.Context, obj client.Object, _ client.Patch, _ ...client.PatchOption) error {
	return t(ctx, obj)
}

func (t testClient) DeleteAllOf(ctx context.Context, obj client.Object, _ ...client.DeleteAllOfOption) error {
	return t(ctx, obj)
}

func (t testClient) Create(ctx context.Context, obj client.Object, _ ...client.CreateOption) error {
	return t(ctx, obj)
}

//nolint:funlen
func Test_http_GetUserAndGroups(t *testing.T) {
	t.Parallel()

	type fields struct {
		Request                   *http.Request
		authTypes                 []request.AuthType
		ignoreGroups              []string
		ignoreImpersonationRegexp *regexp.Regexp
		usernameClaimField        string
		client                    client.Writer
	}

	tests := []struct {
		name         string
		fields       fields
		wantUsername string
		wantGroups   []string
		wantErr      bool
	}{
		{
			name:    "Unauthenticated",
			wantErr: true,
		},
		{
			name: "Certificate",
			fields: fields{
				Request: &http.Request{
					Header: map[string][]string{
						authenticationv1.ImpersonateGroupHeader: {"ImpersonatedGroup"},
						authenticationv1.ImpersonateUserHeader:  {"ImpersonatedUser"},
					},
					TLS: &tls.ConnectionState{
						PeerCertificates: []*x509.Certificate{
							{
								Subject: pkix.Name{
									CommonName: "nobody",
									Organization: []string{
										"group",
									},
								},
							},
						},
					},
				},
				authTypes: []request.AuthType{
					request.BearerToken,
					request.TLSCertificate,
				},
				client: testClient(func(ctx context.Context, obj client.Object) error {
					ac := obj.(*authorizationv1.SubjectAccessReview)
					ac.Status.Allowed = true

					return nil
				}),
			},
			wantUsername: "ImpersonatedUser",
			wantGroups:   []string{"ImpersonatedGroup"},
			wantErr:      false,
		},
		{
			name: "Bearer",
			fields: fields{
				Request: &http.Request{
					Header: map[string][]string{
						"Authorization": {fmt.Sprintf("Bearer %s", "asdf")},
					},
				},
				authTypes: []request.AuthType{
					request.BearerToken,
					request.TLSCertificate,
				},
				usernameClaimField: "",
				client: testClient(func(ctx context.Context, obj client.Object) error {
					return nil
				}),
			},
			wantUsername: "",
			wantGroups:   nil,
			wantErr:      true,
		},
		{
			name: "Certificate-Impersonation",
			fields: fields{
				Request: &http.Request{
					Header: map[string][]string{
						authenticationv1.ImpersonateGroupHeader: {"ImpersonatedGroup", "Regex:Group1", "Regex:Group2", "Regex:DropGroup1", "Regex:DropGroup2"},
						authenticationv1.ImpersonateUserHeader:  {"ImpersonatedUser"},
					},
					TLS: &tls.ConnectionState{
						PeerCertificates: []*x509.Certificate{
							{
								Subject: pkix.Name{
									CommonName: "nobody",
									Organization: []string{
										"group",
									},
								},
							},
						},
					},
				},
				ignoreImpersonationRegexp: regexp.MustCompile("Regex:.*"),
				ignoreGroups:              []string{"Regex:DropGroup1", "Regex:DropGroup2"},
				authTypes: []request.AuthType{
					request.BearerToken,
					request.TLSCertificate,
				},
				client: testClient(func(ctx context.Context, obj client.Object) error {
					ac := obj.(*authorizationv1.SubjectAccessReview)
					ac.Status.Allowed = true

					return nil
				}),
			},
			wantUsername: "ImpersonatedUser",
			wantGroups:   []string{"Regex:Group1", "Regex:Group2"},
			wantErr:      false,
		},
	}
	for _, tt := range tests {
		tc := tt

		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			req := request.NewHTTP(tc.fields.Request, tc.fields.authTypes, tc.fields.usernameClaimField, tc.fields.client, tc.fields.ignoreGroups, tc.fields.ignoreImpersonationRegexp)
			gotUsername, gotGroups, err := req.GetUserAndGroups()
			if (err != nil) != tc.wantErr {
				t.Errorf("GetUserAndGroups() error = %v, wantErr %v", err, tc.wantErr)

				return
			}
			if gotUsername != tc.wantUsername {
				t.Errorf("GetUserAndGroups() gotUsername = %v, want %v", gotUsername, tc.wantUsername)
			}

			sort.Strings(gotGroups)
			sort.Strings(tc.wantGroups)

			if !reflect.DeepEqual(gotGroups, tc.wantGroups) {
				t.Errorf("GetUserAndGroups() gotGroups = %v, want %v", gotGroups, tc.wantGroups)
			}
		})
	}
}
