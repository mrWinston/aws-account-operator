package account

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/iam"
	"github.com/aws/aws-sdk-go/service/organizations"
	"github.com/aws/aws-sdk-go/service/servicequotas"
	"github.com/aws/aws-sdk-go/service/sts"
	"github.com/aws/aws-sdk-go/service/support"
	"github.com/go-logr/logr"
	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"
	apis "github.com/openshift/aws-account-operator/api"
	awsv1alpha1 "github.com/openshift/aws-account-operator/api/v1alpha1"
	"github.com/openshift/aws-account-operator/pkg/awsclient"
	"github.com/openshift/aws-account-operator/pkg/awsclient/mock"
	"github.com/openshift/aws-account-operator/pkg/testutils"
	"github.com/openshift/aws-account-operator/pkg/utils"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type testAccountBuilder struct {
	acct awsv1alpha1.Account
}

type mocks struct {
	fakeKubeClient client.Client
	mockCtrl       *gomock.Controller
	mockAWSClient  *mock.MockClient
}

const (
	TestAccountName      = "testaccount"
	TestAccountNamespace = "testnamespace"
	TestAccountEmail     = "test@example.com"
)

func setupDefaultMocks(t *testing.T, localObjects []runtime.Object) *mocks {
	mocks := &mocks{
		fakeKubeClient: fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(localObjects...).Build(),
		mockCtrl:       gomock.NewController(t),
	}

	mocks.mockAWSClient = mock.NewMockClient(mocks.mockCtrl)

	return mocks
}

func (t *testAccountBuilder) GetTestAccount() *awsv1alpha1.Account {
	return &t.acct
}

func newTestAccountBuilder() *testAccountBuilder {
	return &testAccountBuilder{
		acct: awsv1alpha1.Account{
			TypeMeta: metav1.TypeMeta{},
			ObjectMeta: metav1.ObjectMeta{
				Name:       TestAccountName,
				Namespace:  TestAccountNamespace,
				Labels:     map[string]string{},
				Finalizers: []string{},
				CreationTimestamp: metav1.Time{
					Time: time.Now().Add(-(5 * time.Minute)), // default tests to 5 minute old acct
				},
			},
			Status: awsv1alpha1.AccountStatus{
				State:   string(awsv1alpha1.AccountReady),
				Claimed: false,
			},
			Spec: awsv1alpha1.AccountSpec{},
		},
	}
}

// Just set the whole TypeMeta all in one go
func (t *testAccountBuilder) WithTypetMeta(tm metav1.TypeMeta) *testAccountBuilder {
	t.acct.TypeMeta = tm
	return t
}

// Just set the whole ObjectMeta all in one go
func (t *testAccountBuilder) WithObjectMeta(objm metav1.ObjectMeta) *testAccountBuilder {
	t.acct.ObjectMeta = objm
	return t
}

// Just set the whole Status all in one go
func (t *testAccountBuilder) WithStatus(status awsv1alpha1.AccountStatus) *testAccountBuilder {
	t.acct.Status = status
	return t
}

func (t *testAccountBuilder) WithServiceQuota(regionalServiceQuotas awsv1alpha1.RegionalServiceQuotas) *testAccountBuilder {
	t.acct.Spec.RegionalServiceQuotas = regionalServiceQuotas
	t.acct.Status.RegionalServiceQuotas = make(awsv1alpha1.RegionalServiceQuotas)
	return t
}

// Just set the whole Spec all in one go
func (t *testAccountBuilder) WithSpec(spec awsv1alpha1.AccountSpec) *testAccountBuilder {
	t.acct.Spec = spec
	return t
}

// Set a creation timestamp
func (t *testAccountBuilder) WithCreationTimeStamp(timestamp time.Time) *testAccountBuilder {
	t.acct.ObjectMeta.CreationTimestamp.Time = timestamp
	return t
}

// Set a deletion timestamp
func (t *testAccountBuilder) WithDeletionTimeStamp(timestamp time.Time) *testAccountBuilder {
	t.acct.ObjectMeta.DeletionTimestamp = &metav1.Time{Time: timestamp}
	return t
}

// Add finalizers
func (t *testAccountBuilder) WithFinalizers(finalizers []string) *testAccountBuilder {
	t.acct.ObjectMeta.Finalizers = finalizers
	return t
}

// Add labels
func (t *testAccountBuilder) WithLabels(labels map[string]string) *testAccountBuilder {
	t.acct.ObjectMeta.Labels = labels
	return t
}

// Add a state string
func (t *testAccountBuilder) WithState(state awsv1alpha1.AccountConditionType) *testAccountBuilder {
	t.acct.Status.State = string(state)
	return t
}

// Delete state
func (t *testAccountBuilder) WithoutState() *testAccountBuilder {
	t.acct.Status.State = ""
	return t
}

// Set account claimed or not
func (t *testAccountBuilder) Claimed(claimed bool) *testAccountBuilder {
	t.acct.Status.Claimed = claimed
	return t
}

// Add supportCaseID
func (t *testAccountBuilder) WithSupportCaseID(id string) *testAccountBuilder {
	t.acct.Status.SupportCaseID = id
	return t
}

// Set rotate credentials or not
func (t *testAccountBuilder) RotateCredentials(rotate bool) *testAccountBuilder {
	t.acct.Status.RotateCredentials = rotate
	return t
}

// Set rotate console credentials or not
func (t *testAccountBuilder) RotateConsoleCredentials(rotate bool) *testAccountBuilder {
	t.acct.Status.RotateConsoleCredentials = rotate
	return t
}

// Add a claimLink
func (t *testAccountBuilder) WithClaimLink(link string) *testAccountBuilder {
	t.acct.Spec.ClaimLink = link
	return t
}

// Add a claimLink namespace
func (t *testAccountBuilder) WithClaimLinkNamespace(ns string) *testAccountBuilder {
	t.acct.Spec.ClaimLinkNamespace = ns
	return t
}

// Set BYOC or not
func (t *testAccountBuilder) BYOC(byoc bool) *testAccountBuilder {
	t.acct.Spec.BYOC = byoc
	return t
}

// Add an awsAccountID
func (t *testAccountBuilder) WithAwsAccountID(id string) *testAccountBuilder {
	t.acct.Spec.AwsAccountID = id
	return t
}

func TestMatchSubstring(t *testing.T) {
	tests := []struct {
		name     string
		roleID   string
		role     string
		expected bool
	}{
		{
			name:     "Match substrings 0",
			roleID:   "AROA3SYAY5EP3KG4G2FIR",
			role:     "AROA3SYAY5EP3KG4G2FIR:awsAccountOperator",
			expected: true,
		},
		{
			name:     "Match substrings 1",
			roleID:   "AROA3SYABCEDRKG4G2FIR",
			role:     "AROA3SYABCEDRKG4G2FIR:awsAccountOperator",
			expected: true,
		},
		{
			name:     "Match substrings 2",
			roleID:   "AROABIGORGOHOME4G2FIR",
			role:     "AROABIGORGOHOME4G2FIR:awsAccountOperator",
			expected: true,
		},
		{
			name:     "Match substrings 3",
			roleID:   "IHEHRHSHY5EP3KG4G2FIR",
			role:     "AROA3SYAY5EP3KG4G2FIR:awsAccountOperator",
			expected: false,
		},
		{
			name:     "Match substrings 4",
			roleID:   "AROA3SYAEHAIRHHALKBCDERKG422FIR",
			role:     "AROA3SYABCEDRKG4G2FIR:awsAccountOperator",
			expected: false,
		},
		{
			name:     "Match substrings 5",
			roleID:   "A test string",
			role:     "AROA3SYAY5EP3KG4G2FIR:awsAccountOperator",
			expected: false,
		},
	}
	for _, pair := range tests {
		t.Run(
			pair.name,
			func(t *testing.T) {
				result, err := matchSubstring(pair.roleID, pair.role)
				if result != pair.expected {
					t.Error(
						"For", fmt.Sprintf("%s - %s", pair.roleID, pair.role),
						"expected", pair.expected,
						"got", result,
					)
				}
				if err != nil {
					t.Error(
						"For", fmt.Sprintf("%s - %s", pair.roleID, pair.role),
						"expected", nil,
						"got", err,
					)
				}
			},
		)
	}
}

// Test accountHasState
func TestAccountHasState(t *testing.T) {
	tests := []struct {
		name     string
		expected bool
		acct     *testAccountBuilder
	}{
		{
			name:     "Account has State",
			acct:     newTestAccountBuilder(),
			expected: true,
		},
		{
			name:     "Account does not have State",
			acct:     newTestAccountBuilder().WithoutState(),
			expected: false,
		},
	}
	for _, test := range tests {
		t.Run(
			test.name,
			func(t *testing.T) {
				result := test.acct.acct.HasState()
				if result != test.expected {
					t.Error(
						"for account:", test.acct,
						"expected", test.expected,
						"got", result,
					)
				}
			},
		)
	}
}

// Test the account has SupportCaseID
func TestAccountHasSupportCaseID(t *testing.T) {
	tests := []struct {
		name     string
		expected bool
		acct     *testAccountBuilder
	}{
		{
			name:     "Account has Support CaseID",
			acct:     newTestAccountBuilder().WithSupportCaseID("fakeSupportCaseID"),
			expected: true,
		},
		{
			name:     "Account does not have Support CaseID",
			acct:     newTestAccountBuilder(),
			expected: false,
		},
	}
	for _, test := range tests {
		t.Run(
			test.name,
			func(t *testing.T) {
				result := test.acct.acct.HasSupportCaseID()
				if result != test.expected {
					t.Error(
						"for account:", test.acct,
						"expected", test.expected,
						"got", result,
					)
				}
			},
		)
	}
}

// Test accountIsPendingVerification
func TestAccountIsPendingVerification(t *testing.T) {
	tests := []struct {
		name     string
		expected bool
		acct     *testAccountBuilder
	}{
		{
			name:     "Account is pending verification",
			acct:     newTestAccountBuilder().WithState(awsv1alpha1.AccountPendingVerification),
			expected: true,
		},
		{
			name:     "Account is not pending verificatio",
			acct:     newTestAccountBuilder(),
			expected: false,
		},
	}
	for _, test := range tests {
		t.Run(
			test.name,
			func(t *testing.T) {
				result := test.acct.acct.IsPendingVerification()
				if result != test.expected {
					t.Error(
						"for account:", test.acct,
						"expected", test.expected,
						"got", result,
					)
				}
			},
		)
	}
}

// Test accountIsReady
func TestAccountIsReady(t *testing.T) {
	tests := []struct {
		name     string
		expected bool
		acct     *testAccountBuilder
	}{
		{
			name:     "Account is ready",
			acct:     newTestAccountBuilder(),
			expected: true,
		},
		{
			name:     "Account is not ready",
			acct:     newTestAccountBuilder().WithState(awsv1alpha1.AccountPending),
			expected: false,
		},
	}
	for _, test := range tests {
		t.Run(
			test.name,
			func(t *testing.T) {
				result := test.acct.acct.IsReady()
				if result != test.expected {
					t.Error(
						"for account:", test.acct,
						"expected", test.expected,
						"got", result,
					)
				}
			},
		)
	}
}

// Test accountIsFailed
func TestAccountIsFailed(t *testing.T) {
	tests := []struct {
		name     string
		expected bool
		acct     *testAccountBuilder
	}{
		{
			name:     "Account is ready",
			acct:     newTestAccountBuilder(),
			expected: false,
		},
		{
			name:     "Account is failed",
			acct:     newTestAccountBuilder().WithState(awsv1alpha1.AccountFailed),
			expected: true,
		},
	}
	for _, test := range tests {
		t.Run(
			test.name,
			func(t *testing.T) {
				result := test.acct.acct.IsFailed()
				if result != test.expected {
					t.Error(
						"for account:", test.acct,
						"expected", test.expected,
						"got", result,
					)
				}
			},
		)
	}
}

// Test accountIsCreating
func TestAccountIsCreating(t *testing.T) {
	tests := []struct {
		name     string
		expected bool
		acct     *testAccountBuilder
	}{
		{
			name:     "Account is creating",
			acct:     newTestAccountBuilder().WithState(awsv1alpha1.AccountCreating),
			expected: true,
		},
		{
			name:     "Account is not creating",
			acct:     newTestAccountBuilder().WithState(awsv1alpha1.AccountPending),
			expected: false,
		},
	}
	for _, test := range tests {
		t.Run(
			test.name,
			func(t *testing.T) {
				result := test.acct.acct.IsCreating()
				if result != test.expected {
					t.Error(
						"for account:", test.acct,
						"expected", test.expected,
						"got", result,
					)
				}
			},
		)
	}
}

// Test accountIsClaimed
func TestAccountIsClaimed(t *testing.T) {
	tests := []struct {
		name     string
		expected bool
		acct     *testAccountBuilder
	}{
		{
			name:     "Account is claimed",
			acct:     newTestAccountBuilder().Claimed(true),
			expected: true,
		},
		{
			name:     "Account is not claimed",
			acct:     newTestAccountBuilder(),
			expected: false,
		},
	}
	for _, test := range tests {
		t.Run(
			test.name,
			func(t *testing.T) {
				result := test.acct.acct.IsClaimed()
				if result != test.expected {
					t.Error(
						"for account:", test.acct,
						"expected", test.expected,
						"got", result,
					)
				}
			},
		)
	}
}

// Test accountHasClaimlink
func TestAccountHasClaimLink(t *testing.T) {
	tests := []struct {
		name     string
		expected bool
		acct     *testAccountBuilder
	}{
		{
			name:     "Account has claimLink",
			acct:     newTestAccountBuilder().WithClaimLink("fakeClaimLink"),
			expected: true,
		},
		{
			name:     "Account does not have claimLink",
			acct:     newTestAccountBuilder(),
			expected: false,
		},
	}
	for _, test := range tests {
		t.Run(
			test.name,
			func(t *testing.T) {
				result := test.acct.acct.HasClaimLink()
				if result != test.expected {
					t.Error(
						"for account:", test.acct,
						"expected", test.expected,
						"got", result,
					)
				}
			},
		)
	}
}

// Test accountCreatingTooLong
func TestAccountCreatingToolong(t *testing.T) {
	tests := []struct {
		name     string
		expected bool
		acct     *testAccountBuilder
	}{
		{
			name: "Account creating too long",
			acct: newTestAccountBuilder().WithStatus(awsv1alpha1.AccountStatus{
				State: string(awsv1alpha1.AccountCreating),
				Conditions: []awsv1alpha1.AccountCondition{
					{
						Type:          awsv1alpha1.AccountCreating,
						LastProbeTime: metav1.Time{Time: time.Now().Add(-(createPendTime + time.Minute))},
					},
				},
			}), // 1 minute longer than the allowed timeout
			expected: true,
		},
		{
			name: "Account outside timeout threshold, but not creating",
			acct: newTestAccountBuilder().WithStatus(awsv1alpha1.AccountStatus{
				State: string(awsv1alpha1.AccountReady),
				Conditions: []awsv1alpha1.AccountCondition{
					{
						Type:          awsv1alpha1.AccountCreating,
						LastProbeTime: metav1.Time{Time: time.Now().Add(-(createPendTime + time.Minute))},
					},
				},
			}), // 1 minute longer than the allowed timeout
			expected: false,
		},
		{
			name: "Account creating within timout threshold",
			acct: newTestAccountBuilder().WithStatus(awsv1alpha1.AccountStatus{
				State: string(awsv1alpha1.AccountCreating),
				Conditions: []awsv1alpha1.AccountCondition{
					{
						Type:          awsv1alpha1.AccountCreating,
						LastProbeTime: metav1.Time{Time: time.Now()},
					},
				},
			}),
			expected: false,
		},
		{
			name:     "Account not creating and within timout threshold",
			acct:     newTestAccountBuilder(),
			expected: false,
		},
	}
	for _, test := range tests {
		t.Run(
			test.name,
			func(t *testing.T) {
				result := test.acct.acct.IsCreating() && utils.CreationConditionOlderThan(test.acct.acct, createPendTime)
				if result != test.expected {
					t.Error(
						"for account:", test.acct,
						"expected", test.expected,
						"got", result,
					)
				}
			},
		)
	}
}

// Test accountIsPendingDeletion
func TestAccountIsPendingDeletion(t *testing.T) {
	tests := []struct {
		name     string
		expected bool
		acct     *testAccountBuilder
	}{
		{
			name:     "Account has Deletion Timestamp",
			acct:     newTestAccountBuilder().WithDeletionTimeStamp(time.Now()),
			expected: true,
		},
		{
			name:     "Account does not have Deletion Timestamp",
			acct:     newTestAccountBuilder().BYOC(false),
			expected: false,
		},
	}
	for _, test := range tests {
		t.Run(
			test.name,
			func(t *testing.T) {
				result := test.acct.acct.IsPendingDeletion()
				if result != test.expected {
					t.Error(
						"for account:", test.acct,
						"expected", test.expected,
						"got", result,
					)
				}
			},
		)
	}
}

// Test accountIsBYOC
func TestAccountIsBYOC(t *testing.T) {
	tests := []struct {
		name     string
		expected bool
		acct     *testAccountBuilder
	}{
		{
			name:     "Account BYOC spec is unset",
			acct:     newTestAccountBuilder(),
			expected: false,
		},
		{
			name:     "Account BYOC spec is false",
			acct:     newTestAccountBuilder().BYOC(false),
			expected: false,
		},
		{
			name:     "Account BYOC spec is true",
			acct:     newTestAccountBuilder().BYOC(true),
			expected: true,
		},
	}
	for _, test := range tests {
		t.Run(
			test.name,
			func(t *testing.T) {
				result := test.acct.acct.IsBYOC()
				if result != test.expected {
					t.Error(
						"for account:", test.acct,
						"expected", test.expected,
						"got", result,
					)
				}
			},
		)
	}
}

// Test accountHasAwsv1alpha1Finalizer
func TestAccountHasAwsv1alpha1Finalizer(t *testing.T) {
	tests := []struct {
		name     string
		expected bool
		acct     *testAccountBuilder
	}{
		{
			name:     "Account has v1alpha1 Finalizer",
			acct:     newTestAccountBuilder().WithFinalizers([]string{awsv1alpha1.AccountFinalizer, "fakeFinalizer0", "fakeFinalizer1"}),
			expected: true,
		},
		{
			name:     "Account has only v1alpha1 Finalizer",
			acct:     newTestAccountBuilder().WithFinalizers([]string{awsv1alpha1.AccountFinalizer}),
			expected: true,
		},
		{
			name:     "Account does not have awsv1alpha1 Finalizer",
			acct:     newTestAccountBuilder().WithFinalizers([]string{"fakeFinalizer0", "fakeFinalizer1"}),
			expected: false,
		},
		{
			name:     "Account has no finalizers",
			acct:     newTestAccountBuilder(),
			expected: false,
		},
	}
	for _, test := range tests {
		t.Run(
			test.name,
			func(t *testing.T) {
				result := test.acct.acct.HasAwsv1alpha1Finalizer()
				if result != test.expected {
					t.Error(
						"for account:", test.acct,
						"expected", test.expected,
						"got", result,
					)
				}
			},
		)
	}
}

// Test the accountHasAwsAccountID
func TestAccountHasAwsAccountID(t *testing.T) {
	tests := []struct {
		name     string
		expected bool
		acct     *testAccountBuilder
	}{
		{
			name:     "Account has AWS Account ID",
			acct:     newTestAccountBuilder().WithAwsAccountID("fakeAwsAccountID"),
			expected: true,
		},
		{
			name:     "Account does not have AWS Account ID",
			acct:     newTestAccountBuilder(),
			expected: false,
		},
	}
	for _, test := range tests {
		t.Run(
			test.name,
			func(t *testing.T) {
				result := test.acct.acct.HasAwsAccountID()
				if result != test.expected {
					t.Error(
						"for account:", test.acct,
						"expected", test.expected,
						"got", result,
					)
				}
			},
		)
	}
}

// Test tagAccount
func TestTagAccount(t *testing.T) {
	mocks := setupDefaultMocks(t, []runtime.Object{})

	mockAWSClient := mock.NewMockClient(mocks.mockCtrl)
	accountID := "111111111111"
	hivename := "hivename"

	awsOutputTag := &organizations.TagResourceOutput{}

	mockAWSClient.EXPECT().TagResource(&organizations.TagResourceInput{
		ResourceId: &accountID,
		Tags: []*organizations.Tag{
			{
				Key:   aws.String("owner"),
				Value: aws.String(hivename)}},
	}).Return(
		awsOutputTag,
		nil,
	)

	r := &AccountReconciler{shardName: "hivename"}
	err := TagAccount(mockAWSClient, accountID, r.shardName)
	if err != nil {
		t.Errorf("failed to tag account")
	}
}

// Test accountIsReadyUnclaimedAndHasClaimLink
func TestAccountIsReadyUnclaimedAndHasClaimLink(t *testing.T) {
	tests := []struct {
		name     string
		expected bool
		acct     *testAccountBuilder
	}{
		{
			name:     "Account is ready, unclaimed, and has a claimLink",
			acct:     newTestAccountBuilder().WithClaimLink("fakeClaimLink"),
			expected: true,
		},
		{
			name:     "Account is not ready, unclaimed, and has a claimLink",
			acct:     newTestAccountBuilder().WithState(awsv1alpha1.AccountPending).WithClaimLink("fakeClaimLink"),
			expected: false,
		},
		{
			name:     "Account is ready, claimed, and has a claimLink",
			acct:     newTestAccountBuilder().Claimed(true).WithClaimLink("fakeClaimLink"),
			expected: false,
		},
		{
			name:     "Account is ready, unclaimed, and does not a claimLink",
			acct:     newTestAccountBuilder(),
			expected: false,
		},
	}
	for _, test := range tests {
		t.Run(
			test.name,
			func(t *testing.T) {
				result := test.acct.acct.IsReadyUnclaimedAndHasClaimLink()
				if result != test.expected {
					t.Error(
						"for account:", test.acct,
						"expected", test.expected,
						"got", result,
					)
				}
			},
		)
	}
}

// Test accountISBYOCPendingDeletionWithFinalizer
func TestAccountIsBYOCPendingDeletionWithFinalizer(t *testing.T) {
	tests := []struct {
		name     string
		expected bool
		acct     *testAccountBuilder
	}{
		{
			name:     "Account is a BYOC Account, Pending Deletion and has the awsv1alpha1 Finalizer",
			acct:     newTestAccountBuilder().BYOC(true).WithDeletionTimeStamp(time.Now()).WithFinalizers([]string{awsv1alpha1.AccountFinalizer}),
			expected: true,
		},
		{
			name:     "Account is not a BYOC Account, is Pending Deletion and has the awsv1alpha1 Finalizer",
			acct:     newTestAccountBuilder().WithDeletionTimeStamp(time.Now()).WithFinalizers([]string{awsv1alpha1.AccountFinalizer}),
			expected: false,
		},
		{
			name:     "Account is a BYOC Account, is not Pending Deletion and has the awsv1alpha1 Finalizer",
			acct:     newTestAccountBuilder().BYOC(true).WithFinalizers([]string{awsv1alpha1.AccountFinalizer}),
			expected: false,
		},
		{
			name:     "Account is a BYOC Account, is Pending Deletion but does not have the awsv1alpha1 Finalizer",
			acct:     newTestAccountBuilder().BYOC(true).WithDeletionTimeStamp(time.Now()),
			expected: false,
		},
		{
			name:     "Account is not BYOC, is not Pending Deletion and does not have the awsv1alpha1 Finalizer",
			acct:     newTestAccountBuilder(),
			expected: false,
		},
	}
	for _, test := range tests {
		t.Run(
			test.name,
			func(t *testing.T) {
				result := test.acct.acct.IsBYOCPendingDeletionWithFinalizer()
				if result != test.expected {
					t.Error(
						"for account:", test.acct,
						"expected", test.expected,
						"got", result,
					)
				}
			},
		)
	}
}

// Test accountIsBYOCAndNotReady
func TestAccountIsBYOCAndNotReady(t *testing.T) {
	tests := []struct {
		name     string
		expected bool
		acct     *testAccountBuilder
	}{
		{
			name:     "Account is BYOC and not ready",
			acct:     newTestAccountBuilder().BYOC(true).WithState(awsv1alpha1.AccountCreating),
			expected: true,
		},
		{
			name:     "Account not BYOC or ready",
			acct:     newTestAccountBuilder().WithState(awsv1alpha1.AccountCreating),
			expected: false,
		},
		{
			name:     "Account is BYOC and ready",
			acct:     newTestAccountBuilder().BYOC(true),
			expected: false,
		},
		{
			name:     "Account is not BYOC but is ready",
			acct:     newTestAccountBuilder(),
			expected: false,
		},
	}
	for _, test := range tests {
		t.Run(
			test.name,
			func(t *testing.T) {
				result := test.acct.acct.IsBYOCAndNotReady()
				if result != test.expected {
					t.Error(
						"for account:", test.acct,
						"expected", test.expected,
						"got", result,
					)
				}
			},
		)
	}
}

// Test accountReadyForInitialization
func TestAccountReadyForInitialization(t *testing.T) {
	tests := []struct {
		name     string
		expected bool
		acct     *testAccountBuilder
	}{
		{
			name:     "Account is BYOC and not ready",
			acct:     newTestAccountBuilder().BYOC(true).WithoutState(),
			expected: true,
		},
		{
			name:     "Account is unclaimed and creating",
			acct:     newTestAccountBuilder().WithState(awsv1alpha1.AccountCreating),
			expected: true,
		},
		{
			name:     "Account is BYOC and ready",
			acct:     newTestAccountBuilder().BYOC(true),
			expected: false,
		},
		{
			name:     "Account is not BYOC but is ready",
			acct:     newTestAccountBuilder(),
			expected: false,
		},
		{
			name:     "Account is claimed and creating",
			acct:     newTestAccountBuilder().Claimed(true).WithState(awsv1alpha1.AccountCreating),
			expected: false,
		},
		{
			name:     "Account unclaimed and not creating",
			acct:     newTestAccountBuilder().WithState(awsv1alpha1.AccountReady),
			expected: false,
		},
	}
	for _, test := range tests {
		t.Run(
			test.name,
			func(t *testing.T) {
				result := test.acct.acct.ReadyForInitialization()
				if result != test.expected {
					t.Error(
						"for account:", test.acct,
						"expected", test.expected,
						"got", result,
					)
				}
			},
		)
	}
}

// Test accountIsUnclaimedAndHasNoState
func TestAccountIsUnclaimedAndHasNoState(t *testing.T) {
	tests := []struct {
		name     string
		expected bool
		acct     *testAccountBuilder
	}{
		{
			name:     "Account is unclaimed and has no state",
			acct:     newTestAccountBuilder().WithoutState(),
			expected: true,
		},
		{
			name:     "Account is unclaimed and has state",
			acct:     newTestAccountBuilder(),
			expected: false,
		},
		{
			name:     "Account is claimed and has no state",
			acct:     newTestAccountBuilder().Claimed(true).WithoutState(),
			expected: false,
		},
		{
			name:     "Account is claimed and has state",
			acct:     newTestAccountBuilder().Claimed(true),
			expected: false,
		},
	}
	for _, test := range tests {
		t.Run(
			test.name,
			func(t *testing.T) {
				result := test.acct.acct.IsUnclaimedAndHasNoState()
				if result != test.expected {
					t.Error(
						"for account:", test.acct,
						"expected", test.expected,
						"got", result,
					)
				}
			},
		)
	}
}

// Test accountIsUnclaimedAndIsCreating
func TestAccountIsUnclaimedAndCreating(t *testing.T) {
	tests := []struct {
		name     string
		expected bool
		acct     *testAccountBuilder
	}{
		{
			name:     "Account is unclaimed and Creating",
			acct:     newTestAccountBuilder().WithState(awsv1alpha1.AccountCreating),
			expected: true,
		},
		{
			name:     "Account is unclaimed and not creating",
			acct:     newTestAccountBuilder(),
			expected: false,
		},
		{
			name:     "Account is claimed and Creating",
			acct:     newTestAccountBuilder().Claimed(true).WithState(awsv1alpha1.AccountCreating),
			expected: false,
		},
		{
			name:     "Account is claimed and not creating",
			acct:     newTestAccountBuilder().Claimed(true),
			expected: false,
		},
	}
	for _, test := range tests {
		t.Run(
			test.name,
			func(t *testing.T) {
				result := test.acct.acct.IsUnclaimedAndIsCreating()
				if result != test.expected {
					t.Error(
						"for account:", test.acct,
						"expected", test.expected,
						"got", result,
					)
				}
			},
		)
	}
}

func TestGetAssumeRole(t *testing.T) {
	tests := []struct {
		name     string
		expected string
		acct     *testAccountBuilder
	}{
		{
			name:     "Get role for BYOC Account",
			acct:     newTestAccountBuilder().BYOC(true).WithLabels(map[string]string{awsv1alpha1.IAMUserIDLabel: "xxxxx"}),
			expected: fmt.Sprintf("%s-%s", awsv1alpha1.ManagedOpenShiftSupportRole, "xxxxx"),
		},
		{
			name:     "Get role for Non-BYOC Account",
			acct:     newTestAccountBuilder(),
			expected: awsv1alpha1.AccountOperatorIAMRole,
		},
	}
	for _, test := range tests {
		t.Run(
			test.name,
			func(t *testing.T) {
				result := test.acct.acct.GetAssumeRole()
				if result != test.expected {
					t.Error(
						"for account:", test.acct,
						"expected", test.expected,
						"got", result,
					)
				}
			},
		)
	}
}

// Test GetOpenRegionalQuotaIncreaseRequestsRef
func TestGetOpenRegionalQuotaIncreaseRequestsRef(t *testing.T) {
	tests := []struct {
		name     string
		expected int
		acct     *testAccountBuilder
	}{
		{
			name: "GetOpenRegionalQuotaIncreaseRequestsRef",
			acct: newTestAccountBuilder().WithStatus(
				awsv1alpha1.AccountStatus{
					RegionalServiceQuotas: awsv1alpha1.RegionalServiceQuotas{
						"us-east-1": awsv1alpha1.AccountServiceQuota{
							awsv1alpha1.RunningStandardInstances: {
								Value:  10,
								Status: awsv1alpha1.ServiceRequestTodo,
							},
						},
						"us-east-2": awsv1alpha1.AccountServiceQuota{
							awsv1alpha1.RunningStandardInstances: {
								Value:  20,
								Status: awsv1alpha1.ServiceRequestTodo,
							},
							awsv1alpha1.EC2VPCElasticIPsQuotaCode: {
								Value:  11,
								Status: awsv1alpha1.ServiceRequestTodo,
							},
						},
						"us-west-1": awsv1alpha1.AccountServiceQuota{
							awsv1alpha1.RunningStandardInstances: {
								Value:  10,
								Status: awsv1alpha1.ServiceRequestCompleted,
							},
						},
					},
				},
			),
			expected: 3,
		},
	}
	for _, test := range tests {
		t.Run(
			test.name,
			func(t *testing.T) {
				count, _ := test.acct.acct.GetQuotaRequestsByStatus(awsv1alpha1.ServiceRequestTodo)
				if count != test.expected {
					t.Error(
						"for account:", test.acct,
						"expected", test.expected,
						"got", count,
					)
				}
			},
		)
	}
}

// Test finalizeAccount
func TestFinalizeAccount(t *testing.T) {

	err := apis.AddToScheme(scheme.Scheme)
	if err != nil {
		fmt.Printf("failed adding to scheme in account_controller_test.go")
	}

	nullLogger := testutils.NewTestLogger().Logger()

	tests := []struct {
		name string
		acct *testAccountBuilder
	}{
		{
			name: "Account has STS Mode enabled",
			acct: newTestAccountBuilder().WithSpec(awsv1alpha1.AccountSpec{ManualSTSMode: true}),
		},
		{
			name: "Account is BYOC without iamUserId Labels",
			acct: newTestAccountBuilder().BYOC(true),
		},
		{
			name: "Account is non-BYOC, non-STS",
			acct: newTestAccountBuilder(),
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			//t.Parallel()

			localObjects := []runtime.Object{
				&test.acct.acct,
			}

			mocks := setupDefaultMocks(t, localObjects)
			mockAWSClient := mock.NewMockClient(mocks.mockCtrl)

			// This is necessary for the mocks to report failures like methods not being called an expected number of times.
			// after mocks is defined
			defer mocks.mockCtrl.Finish()

			r := AccountReconciler{
				Client: mocks.fakeKubeClient,
				Scheme: scheme.Scheme,
			}

			r.finalizeAccount(nullLogger, mockAWSClient, &test.acct.acct)
		})
	}
}

func TestFinalizeAccount_LabelledBYOCAccount(t *testing.T) {
	err := apis.AddToScheme(scheme.Scheme)
	if err != nil {
		fmt.Printf("failed adding to scheme in account_controller_test.go")
	}
	nullLogger := testutils.NewTestLogger().Logger()

	account := newTestAccountBuilder().BYOC(true).WithLabels(
		map[string]string{
			"iamUserId": "iam1234",
		},
	).acct

	localObjects := []runtime.Object{&account}
	mocks := setupDefaultMocks(t, localObjects)
	mockAWSClient := mock.NewMockClient(mocks.mockCtrl)
	mockAWSClient.EXPECT().ListUsersPages(gomock.Any(), gomock.Any())
	mockAWSClient.EXPECT().ListRoles(gomock.Any()).Return(
		&iam.ListRolesOutput{
			Roles:       []*iam.Role{},
			IsTruncated: aws.Bool(false),
		},
		nil,
	)

	// This is necessary for the mocks to report failures like methods not being called an expected number of times.
	// after mocks is defined
	defer mocks.mockCtrl.Finish()

	r := AccountReconciler{
		Client: mocks.fakeKubeClient,
		Scheme: scheme.Scheme,
	}
	r.finalizeAccount(nullLogger, mockAWSClient, &account)
}

var _ = Describe("Account Controller", func() {
	var (
		nullTestLogger testutils.TestLogger
		nullLogger     logr.Logger
		mockAWSClient  *mock.MockClient
		accountName    string
		accountEmail   string
		ctrl           *gomock.Controller
		account        *awsv1alpha1.Account
		configMap      *v1.ConfigMap
		r              *AccountReconciler
		req            reconcile.Request
	)

	err := apis.AddToScheme(scheme.Scheme)
	if err != nil {
		fmt.Printf("failed adding apis to scheme in account controller test")
	}

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		accountName = TestAccountName
		accountEmail = TestAccountEmail
		nullTestLogger = testutils.NewTestLogger()
		nullLogger = nullTestLogger.Logger()
		mockAWSClient = mock.NewMockClient(ctrl)
		configMap = &v1.ConfigMap{
			TypeMeta: metav1.TypeMeta{},
			ObjectMeta: metav1.ObjectMeta{
				Name:        awsv1alpha1.DefaultConfigMap,
				Namespace:   awsv1alpha1.AccountCrNamespace,
				Labels:      map[string]string{},
				Annotations: map[string]string{},
			},
			Data: map[string]string{
				"ami-owner": "12345",
			},
		}
		r = &AccountReconciler{
			Scheme: scheme.Scheme,
			awsClientBuilder: &mock.Builder{
				MockController: ctrl,
			},
			shardName: "hivename",
		}
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	Context("Testing CreateAccount", func() {

		It("AWS returns ErrCodeConstraintViolationException from CreateAccount", func() {
			// ErrCodeConstraintViolationException is mapped to awsv1alpha1.ErrAwsAccountLimitExceeded in CreateAccount
			mockAWSClient.EXPECT().CreateAccount(gomock.Any()).Return(nil, awserr.New(organizations.ErrCodeConstraintViolationException, "Error String", nil))
			createAccountOutput, err := CreateAccount(nullLogger, mockAWSClient, accountName, accountEmail)
			Expect(err).To(HaveOccurred())
			Expect(createAccountOutput).To(Equal(&organizations.DescribeCreateAccountStatusOutput{}))
			Expect(awsv1alpha1.ErrAwsAccountLimitExceeded).To(Equal(err))
			Expect(nullTestLogger.Messages()).Should(ContainElement(ContainSubstring(organizations.ErrCodeConstraintViolationException)))
		})

		It("AWS returns ErrCodeServiceException from CreateAccount", func() {
			// ErrCodeServiceException is mapped to awsv1alpha1.ErrAwsInternalFailure in CreateAccount
			mockAWSClient.EXPECT().CreateAccount(gomock.Any()).Return(nil, awserr.New(organizations.ErrCodeServiceException, "Error String", nil))
			createAccountOutput, err := CreateAccount(nullLogger, mockAWSClient, accountName, accountEmail)
			Expect(err).To(HaveOccurred())
			Expect(createAccountOutput).To(Equal(&organizations.DescribeCreateAccountStatusOutput{}))
			Expect(awsv1alpha1.ErrAwsInternalFailure).To(Equal(err))
			Expect(nullTestLogger.Messages()).Should(ContainElement(ContainSubstring(organizations.ErrCodeServiceException)))
		})

		It("AWS returns ErrCodeTooManyRequestsException from CreateAccount", func() {
			// ErrCodeTooManyRequestsException is mapped to awsv1alpha1.ErrAwsTooManyRequests in CreateAccount
			mockAWSClient.EXPECT().CreateAccount(gomock.Any()).Return(nil, awserr.New(organizations.ErrCodeTooManyRequestsException, "Error String", nil))
			createAccountOutput, err := CreateAccount(nullLogger, mockAWSClient, accountName, accountEmail)
			Expect(err).To(HaveOccurred())
			Expect(createAccountOutput).To(Equal(&organizations.DescribeCreateAccountStatusOutput{}))
			Expect(awsv1alpha1.ErrAwsTooManyRequests).To(Equal(err))
			Expect(nullTestLogger.Messages()).Should(ContainElement(ContainSubstring(organizations.ErrCodeTooManyRequestsException)))
		})

		It("AWS returns error from CreateAccount", func() {
			// Unhandled AWS exceptions get mapped awsv1alpha1.ErrAwsFailedCreateAccount in CreateAccount
			mockAWSClient.EXPECT().CreateAccount(gomock.Any()).Return(nil, awserr.New(organizations.ErrCodeDuplicateAccountException, "Error String", nil))
			createAccountOutput, err := CreateAccount(nullLogger, mockAWSClient, accountName, accountEmail)
			Expect(err).To(HaveOccurred())
			Expect(createAccountOutput).To(Equal(&organizations.DescribeCreateAccountStatusOutput{}))
			Expect(awsv1alpha1.ErrAwsFailedCreateAccount).To(Equal(err))
			Expect(nullTestLogger.Messages()).Should(ContainElement(ContainSubstring(organizations.ErrCodeDuplicateAccountException)))
		})

		It("AWS returns an error from DescribeCreateAccountStatus", func() {
			mockAWSClient.EXPECT().CreateAccount(gomock.Any()).Return(
				&organizations.CreateAccountOutput{
					CreateAccountStatus: &organizations.CreateAccountStatus{
						Id: aws.String("ID"),
					},
				},
				nil,
			)

			expectedErr := awserr.New(organizations.ErrCodeServiceException, "Error String", nil)
			mockAWSClient.EXPECT().DescribeCreateAccountStatus(gomock.Any()).Return(nil, expectedErr) //errors.New("MyError")) //)
			createAccountOutput, err := CreateAccount(nullLogger, mockAWSClient, accountName, accountEmail)
			Expect(err).To(HaveOccurred())
			Expect(createAccountOutput).To(Equal(&organizations.DescribeCreateAccountStatusOutput{}))
			Expect(expectedErr).To(Equal(err))
		})

		It("DescribeCreateAccountStatus returns a FAILED state", func() {
			mockAWSClient.EXPECT().CreateAccount(gomock.Any()).Return(
				&organizations.CreateAccountOutput{
					CreateAccountStatus: &organizations.CreateAccountStatus{
						Id: aws.String("ID"),
					},
				},
				nil,
			)
			describeCreateAccountStatusOutput := &organizations.DescribeCreateAccountStatusOutput{
				CreateAccountStatus: &organizations.CreateAccountStatus{
					State:         aws.String("FAILED"),
					FailureReason: aws.String("ACCOUNT_LIMIT_EXCEEDED"),
				},
			}
			mockAWSClient.EXPECT().DescribeCreateAccountStatus(gomock.Any()).Return(describeCreateAccountStatusOutput, nil)
			createAccountOutput, err := CreateAccount(nullLogger, mockAWSClient, accountName, accountEmail)
			Expect(err).To(HaveOccurred())

			Expect(createAccountOutput).To(Equal(&organizations.DescribeCreateAccountStatusOutput{}))
			Expect(awsv1alpha1.ErrAwsAccountLimitExceeded).To(Equal(err))
		})
		It("CreateAccount creates account", func() {
			mockAWSClient.EXPECT().CreateAccount(gomock.Any()).Return(
				&organizations.CreateAccountOutput{
					CreateAccountStatus: &organizations.CreateAccountStatus{
						Id: aws.String("ID"),
					},
				},
				nil,
			)
			describeCreateAccountStatusOutput := &organizations.DescribeCreateAccountStatusOutput{
				CreateAccountStatus: &organizations.CreateAccountStatus{
					State: aws.String("SUCCEEDED"),
				},
			}
			mockAWSClient.EXPECT().DescribeCreateAccountStatus(gomock.Any()).Return(describeCreateAccountStatusOutput, nil)
			createAccountOutput, err := CreateAccount(nullLogger, mockAWSClient, accountName, accountEmail)
			Expect(err).To(Succeed())
			Expect(createAccountOutput).To(Equal(describeCreateAccountStatusOutput))
			Expect(err).Should(BeNil())
		})
	})

	Context("Testing Reconciliation", func() {
		It("A ready account being claimed adds a claimed status condition", func() {
			account = &newTestAccountBuilder().WithState(AccountReady).WithClaimLink("claimedaccount").acct

			r.Client = fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects([]runtime.Object{account, configMap}...).Build()
			req = reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: account.Namespace,
					Name:      account.Name,
				},
			}

			_, err := r.Reconcile(context.TODO(), req)
			Expect(err).ToNot(HaveOccurred())

			ac := &awsv1alpha1.Account{}
			err = r.Client.Get(context.TODO(), types.NamespacedName{Name: account.Name, Namespace: account.Namespace}, ac)
			Expect(err).ToNot(HaveOccurred())
			Expect(ac.Status.Claimed).To(BeTrue())
			Expect(len(ac.Status.Conditions)).To(Equal(1))
			Expect(ac.Status.Conditions).Should(ContainElement(MatchFields(IgnoreExtras, Fields{
				"Type": Equal(awsv1alpha1.AccountIsClaimed),
			})))
		})

		It("A ready BYOC account being claimed adds a claimed status condition", func() {
			claimName := fmt.Sprintf("%s-%s", accountName, "claim")
			accountClaim := &awsv1alpha1.AccountClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      claimName,
					Namespace: awsv1alpha1.AccountCrNamespace,
				},
				Spec: awsv1alpha1.AccountClaimSpec{
					LegalEntity: awsv1alpha1.LegalEntity{
						Name: "test-legal",
						ID:   "test-legal-123",
					},
					AccountLink: "claimedaccount",
				},
			}
			account = &newTestAccountBuilder().BYOC(true).WithState(AccountReady).WithClaimLink(claimName).
				WithClaimLinkNamespace(awsv1alpha1.AccountCrNamespace).acct

			r.Client = fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects([]runtime.Object{account, accountClaim, configMap}...).Build()

			req = reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: account.Namespace,
					Name:      account.Name,
				},
			}

			_, err := r.Reconcile(context.TODO(), req)
			Expect(err).ToNot(HaveOccurred())

			ac := &awsv1alpha1.Account{}
			err = r.Client.Get(context.TODO(), types.NamespacedName{Name: account.Name, Namespace: account.Namespace}, ac)
			Expect(err).ToNot(HaveOccurred())
			Expect(ac.Status.Claimed).To(BeTrue())
			Expect(ac.Spec.BYOC).To(BeTrue())
			Expect(ac.Status.Conditions).Should(ContainElement(MatchFields(IgnoreExtras, Fields{
				"Type": Equal(awsv1alpha1.AccountIsClaimed),
			})))
		})

	})

	Context("Testing account CR service quotas", func() {
		utils.DetectDevMode = ""
		When("Called with a CCS account", func() {
			account = &newTestAccountBuilder().BYOC(true).WithState(awsv1alpha1.AccountPendingVerification).acct
			It("does nothing", func() {
				_, err := r.HandleNonCCSPendingVerification(nullLogger, account, mockAWSClient)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(Equal("account is BYOC - should not be handled in NonCCS method"))
			})
		})
		When("Called with a non-CCS account", func() {
			BeforeEach(func() {
				account = &newTestAccountBuilder().BYOC(false).WithState(awsv1alpha1.AccountPendingVerification).WithAwsAccountID("4321").acct
				r.Client = fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects([]runtime.Object{account}...).Build()
			})
			When("No service quotas are defined for the account", func() {
				It("does does not open service quota requests for the account", func() {
					mockAWSClient.EXPECT().CreateCase(gomock.Any()).Return(&support.CreateCaseOutput{
						CaseId: aws.String("123456"),
					}, nil)
					mockAWSClient.EXPECT().DescribeCases(gomock.Any()).Return(&support.DescribeCasesOutput{
						Cases: []*support.CaseDetails{
							{
								CaseId: aws.String("123456"),
								Status: aws.String("resolved"),
							},
						},
					}, nil)
					mockAWSClient.EXPECT().RequestServiceQuotaIncrease(gomock.Any()).Times(0)
					Eventually(func() []string {
						_, err := r.HandleNonCCSPendingVerification(nullLogger, account, mockAWSClient)
						Expect(err).NotTo(HaveOccurred())
						return []string{account.Status.State, account.Status.SupportCaseID}
					}).Should(Equal([]string{AccountReady, "123456"}))
				})
			})
			When("Service quotas are defined for the account", func() {
				BeforeEach(func() {
					account = &newTestAccountBuilder().BYOC(false).WithServiceQuota(awsv1alpha1.RegionalServiceQuotas{
						"default": awsv1alpha1.AccountServiceQuota{
							awsv1alpha1.RunningStandardInstances: {
								Value: 100,
							},
						},
					}).WithState(awsv1alpha1.AccountPendingVerification).WithAwsAccountID("4321").acct
					r.Client = fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects([]runtime.Object{account, configMap}...).Build()
				})
				It("copies the service quotas from spec to status", func() {
					subClient := mock.NewMockClient(ctrl)
					AssumeRole = func(
						reqLogger logr.Logger,
						awsClientBuilder awsclient.IBuilder,
						currentAcctInstance *awsv1alpha1.Account,
						client client.Client,
						awsSetupClient awsclient.Client,
						region string,
						roleToAssume string,
						ccsRoleID string) (awsclient.Client, *sts.AssumeRoleOutput, error) {
						return subClient, &sts.AssumeRoleOutput{}, nil
					}
					// Reconciliation loop 1
					subClient.EXPECT().DescribeRegions(gomock.Any()).Return(&ec2.DescribeRegionsOutput{
						Regions: []*ec2.Region{
							{
								RegionName: aws.String("us-east-1"),
							},
						},
					}, nil)
					err := setCurrentAccountServiceQuotas(r, nullLogger, account, mockAWSClient)
					Expect(err).ToNot(HaveOccurred())
					Expect(len(account.Status.RegionalServiceQuotas)).To(Equal(1))
					Expect(len(account.Status.RegionalServiceQuotas["us-east-1"])).To(Equal(1))
				})
				It("errors when called with a unsupported (by us) servicequota", func() {
					account = &newTestAccountBuilder().BYOC(false).WithServiceQuota(awsv1alpha1.RegionalServiceQuotas{
						"default": awsv1alpha1.AccountServiceQuota{
							"Invalid-Code": {
								Value: 100,
							},
						},
					}).WithState(awsv1alpha1.AccountPendingVerification).acct

					subClient := mock.NewMockClient(ctrl)
					AssumeRole = func(
						reqLogger logr.Logger,
						awsClientBuilder awsclient.IBuilder,
						currentAcctInstance *awsv1alpha1.Account,
						client client.Client,
						awsSetupClient awsclient.Client,
						region string,
						roleToAssume string,
						ccsRoleID string) (awsclient.Client, *sts.AssumeRoleOutput, error) {
						return subClient, &sts.AssumeRoleOutput{}, nil
					}

					mockAWSClient.EXPECT().CreateCase(gomock.Any()).Return(&support.CreateCaseOutput{
						CaseId: aws.String("123456"),
					}, nil)
					subClient.EXPECT().DescribeRegions(gomock.Any()).Return(&ec2.DescribeRegionsOutput{
						Regions: []*ec2.Region{
							{
								RegionName: aws.String("us-east-1"),
							},
						},
					}, nil)
					_, err := r.HandleNonCCSPendingVerification(nullLogger, account, mockAWSClient)
					Expect(err).To(HaveOccurred())
				})
				It("does not create a servicequota case if the quota is already higher", func() {
					subClient := mock.NewMockClient(ctrl)
					AssumeRole = func(
						reqLogger logr.Logger,
						awsClientBuilder awsclient.IBuilder,
						currentAcctInstance *awsv1alpha1.Account,
						client client.Client,
						awsSetupClient awsclient.Client,
						region string,
						roleToAssume string,
						ccsRoleID string) (awsclient.Client, *sts.AssumeRoleOutput, error) {
						return subClient, &sts.AssumeRoleOutput{}, nil
					}
					// Reconciliation loop 1
					mockAWSClient.EXPECT().CreateCase(gomock.Any()).Return(&support.CreateCaseOutput{
						CaseId: aws.String("123456"),
					}, nil)
					mockAWSClient.EXPECT().DescribeCases(gomock.Any()).Return(&support.DescribeCasesOutput{
						Cases: []*support.CaseDetails{
							{
								CaseId: aws.String("123456"),
								Status: aws.String("resolved"),
							},
						},
					}, nil).Times(2)
					subClient.EXPECT().DescribeRegions(gomock.Any()).Return(&ec2.DescribeRegionsOutput{
						Regions: []*ec2.Region{
							{
								RegionName: aws.String("us-east-1"),
							},
						},
					}, nil)
					subClient.EXPECT().GetServiceQuota(gomock.Any()).Return(&servicequotas.GetServiceQuotaOutput{
						Quota: &servicequotas.ServiceQuota{
							QuotaCode: aws.String(string(awsv1alpha1.RunningStandardInstances)),
							Value:     aws.Float64(101),
						},
					}, nil)
					subClient.EXPECT().RequestServiceQuotaIncrease(gomock.Any()).Times(0)
					Eventually(func() []string {
						_, err := r.HandleNonCCSPendingVerification(nullLogger, account, mockAWSClient)
						Expect(err).NotTo(HaveOccurred())
						return []string{account.Status.State, account.Status.SupportCaseID}
					}, 60*time.Second).Should(Equal([]string{AccountReady, "123456"}))
				})
				It("creates a servicequota case for each defined quota", func() {
					subClient := mock.NewMockClient(ctrl)
					AssumeRole = func(
						reqLogger logr.Logger,
						awsClientBuilder awsclient.IBuilder,
						currentAcctInstance *awsv1alpha1.Account,
						client client.Client,
						awsSetupClient awsclient.Client,
						region string,
						roleToAssume string,
						ccsRoleID string) (awsclient.Client, *sts.AssumeRoleOutput, error) {
						return subClient, &sts.AssumeRoleOutput{}, nil
					}
					// Reconciliation loop 1
					mockAWSClient.EXPECT().CreateCase(gomock.Any()).Return(&support.CreateCaseOutput{
						CaseId: aws.String("123456"),
					}, nil)
					mockAWSClient.EXPECT().DescribeCases(gomock.Any()).Return(&support.DescribeCasesOutput{
						Cases: []*support.CaseDetails{
							{
								CaseId: aws.String("123456"),
								Status: aws.String("resolved"),
							},
						},
					}, nil).Times(2)
					subClient.EXPECT().DescribeRegions(gomock.Any()).Return(&ec2.DescribeRegionsOutput{
						Regions: []*ec2.Region{
							{
								RegionName: aws.String("us-east-1"),
							},
						},
					}, nil)
					subClient.EXPECT().ListRequestedServiceQuotaChangeHistoryByQuota(gomock.Any()).Return(&servicequotas.ListRequestedServiceQuotaChangeHistoryByQuotaOutput{
						RequestedQuotas: []*servicequotas.RequestedServiceQuotaChange{},
					}, nil)
					subClient.EXPECT().GetServiceQuota(gomock.Any()).Return(&servicequotas.GetServiceQuotaOutput{
						Quota: &servicequotas.ServiceQuota{
							QuotaCode: aws.String(string(awsv1alpha1.RunningStandardInstances)),
							Value:     aws.Float64(0),
						},
					}, nil)
					subClient.EXPECT().RequestServiceQuotaIncrease(gomock.Any()).Return(&servicequotas.RequestServiceQuotaIncreaseOutput{
						RequestedQuota: &servicequotas.RequestedServiceQuotaChange{
							CaseId: aws.String("234567"),
						},
					}, nil)
					// Reconciliation loop 2
					mockAWSClient.EXPECT().DescribeCases(gomock.Any()).Return(&support.DescribeCasesOutput{
						Cases: []*support.CaseDetails{
							{
								CaseId: aws.String("123456"),
								Status: aws.String("resolved"),
							},
						},
					}, nil)
					// The quota now matches the requested value the case is finished
					subClient.EXPECT().GetServiceQuota(gomock.Any()).Return(&servicequotas.GetServiceQuotaOutput{
						Quota: &servicequotas.ServiceQuota{
							QuotaCode: aws.String(string(awsv1alpha1.RunningStandardInstances)),
							Value:     aws.Float64(100),
						},
					}, nil)
					Eventually(func() []string {
						_, err = r.HandleNonCCSPendingVerification(nullLogger, account, mockAWSClient)
						Expect(err).NotTo(HaveOccurred())
						return []string{account.Status.State, account.Status.SupportCaseID}
					}).Should(Equal([]string{AccountReady, "123456"}))
					var k8sAccount awsv1alpha1.Account
					_ = r.Client.Get(context.TODO(), types.NamespacedName{
						Namespace: TestAccountNamespace,
						Name:      TestAccountName,
					}, &k8sAccount)
					Expect(k8sAccount.Status.State).To(Equal(AccountReady))
				})
				It("moves a servicequota to in-progress if the case is open but not resolved", func() {
					subClient := mock.NewMockClient(ctrl)
					AssumeRole = func(
						reqLogger logr.Logger,
						awsClientBuilder awsclient.IBuilder,
						currentAcctInstance *awsv1alpha1.Account,
						client client.Client,
						awsSetupClient awsclient.Client,
						region string,
						roleToAssume string,
						ccsRoleID string) (awsclient.Client, *sts.AssumeRoleOutput, error) {
						return subClient, &sts.AssumeRoleOutput{}, nil
					}
					// Reconciliation loop 1
					mockAWSClient.EXPECT().CreateCase(gomock.Any()).Return(&support.CreateCaseOutput{
						CaseId: aws.String("123456"),
					}, nil)
					mockAWSClient.EXPECT().DescribeCases(gomock.Any()).Return(&support.DescribeCasesOutput{
						Cases: []*support.CaseDetails{
							{
								CaseId: aws.String("123456"),
								Status: aws.String("resolved"),
							},
						},
					}, nil)
					subClient.EXPECT().DescribeRegions(gomock.Any()).Return(&ec2.DescribeRegionsOutput{
						Regions: []*ec2.Region{
							{
								RegionName: aws.String("us-east-1"),
							},
						},
					}, nil)
					subClient.EXPECT().ListRequestedServiceQuotaChangeHistoryByQuota(gomock.Any()).Return(&servicequotas.ListRequestedServiceQuotaChangeHistoryByQuotaOutput{
						RequestedQuotas: []*servicequotas.RequestedServiceQuotaChange{},
					}, nil)
					subClient.EXPECT().GetServiceQuota(gomock.Any()).Return(&servicequotas.GetServiceQuotaOutput{
						Quota: &servicequotas.ServiceQuota{
							QuotaCode: aws.String(string(awsv1alpha1.RunningStandardInstances)),
							Value:     aws.Float64(0),
						},
					}, nil)
					subClient.EXPECT().RequestServiceQuotaIncrease(gomock.Any()).Return(&servicequotas.RequestServiceQuotaIncreaseOutput{
						RequestedQuota: &servicequotas.RequestedServiceQuotaChange{
							CaseId: aws.String("234567"),
						},
					}, nil)
					Eventually(func() []string {
						_, err = r.HandleNonCCSPendingVerification(nullLogger, account, mockAWSClient)
						Expect(err).NotTo(HaveOccurred())
						status := account.Status.RegionalServiceQuotas["us-east-1"][awsv1alpha1.RunningStandardInstances]
						fmt.Printf("%+v\n", status.Status)
						stringStatus := string(status.Status)
						supportCase := account.Status.SupportCaseID
						return []string{stringStatus, supportCase}
					}).Should(Equal([]string{string(awsv1alpha1.ServiceRequestInProgress), "123456"}))
					var k8sAccount awsv1alpha1.Account
					_ = r.Client.Get(context.TODO(), types.NamespacedName{
						Namespace: TestAccountNamespace,
						Name:      TestAccountName,
					}, &k8sAccount)
					Expect(k8sAccount.Status.State).To(Equal(AccountPendingVerification))
				})
				It("updates the correct region if multiple ones get updated", func() {
					subClient := mock.NewMockClient(ctrl)
					AssumeRole = func(
						reqLogger logr.Logger,
						awsClientBuilder awsclient.IBuilder,
						currentAcctInstance *awsv1alpha1.Account,
						client client.Client,
						awsSetupClient awsclient.Client,
						region string,
						roleToAssume string,
						ccsRoleID string) (awsclient.Client, *sts.AssumeRoleOutput, error) {
						return subClient, &sts.AssumeRoleOutput{}, nil
					}
					// Reconciliation loop 1
					mockAWSClient.EXPECT().CreateCase(gomock.Any()).Return(&support.CreateCaseOutput{
						CaseId: aws.String("123456"),
					}, nil)
					subClient.EXPECT().DescribeRegions(gomock.Any()).Return(&ec2.DescribeRegionsOutput{
						Regions: []*ec2.Region{
							{
								RegionName: aws.String("us-east-1"),
							},
							{
								RegionName: aws.String("us-east-2"),
							},
						},
					}, nil)
					_, err = r.HandleNonCCSPendingVerification(nullLogger, account, mockAWSClient)
					Expect(err).ToNot(HaveOccurred())
					Expect(len(account.Status.RegionalServiceQuotas)).To(Equal(2))
					Expect(len(account.Status.RegionalServiceQuotas["us-east-1"])).To(Equal(1))
					Expect(len(account.Status.RegionalServiceQuotas["us-east-2"])).To(Equal(1))
					mockAWSClient.EXPECT().DescribeCases(gomock.Any()).Return(&support.DescribeCasesOutput{
						Cases: []*support.CaseDetails{
							{
								CaseId: aws.String("123456"),
								Status: aws.String("resolved"),
							},
						},
					}, nil).Times(2)
					subClient.EXPECT().ListRequestedServiceQuotaChangeHistoryByQuota(gomock.Any()).Return(&servicequotas.ListRequestedServiceQuotaChangeHistoryByQuotaOutput{
						RequestedQuotas: []*servicequotas.RequestedServiceQuotaChange{},
					}, nil).Times(2)
					// Have to increase both of our quotas
					subClient.EXPECT().GetServiceQuota(gomock.Any()).Return(&servicequotas.GetServiceQuotaOutput{
						Quota: &servicequotas.ServiceQuota{
							QuotaCode: aws.String(string(awsv1alpha1.RunningStandardInstances)),
							Value:     aws.Float64(0),
						},
					}, nil).Times(2)
					subClient.EXPECT().RequestServiceQuotaIncrease(gomock.Any()).Return(&servicequotas.RequestServiceQuotaIncreaseOutput{
						RequestedQuota: &servicequotas.RequestedServiceQuotaChange{
							CaseId: aws.String("234567"),
						},
					}, nil).Times(2)
					_, err = r.HandleNonCCSPendingVerification(nullLogger, account, mockAWSClient)
					Expect(account.Status.RegionalServiceQuotas["us-east-1"][awsv1alpha1.RunningStandardInstances].Status).To(Equal(awsv1alpha1.ServiceRequestInProgress))
					Expect(account.Status.RegionalServiceQuotas["us-east-2"][awsv1alpha1.RunningStandardInstances].Status).To(Equal(awsv1alpha1.ServiceRequestInProgress))
					Expect(account.Status.State).To(Equal(AccountPendingVerification))
					mockAWSClient.EXPECT().DescribeCases(gomock.Any()).Return(&support.DescribeCasesOutput{
						Cases: []*support.CaseDetails{
							{
								CaseId: aws.String("123456"),
								Status: aws.String("resolved"),
							},
						},
					}, nil)
					// Have to increase both of our quotas
					subClient.EXPECT().GetServiceQuota(gomock.Any()).Return(&servicequotas.GetServiceQuotaOutput{
						Quota: &servicequotas.ServiceQuota{
							QuotaCode: aws.String(string(awsv1alpha1.RunningStandardInstances)),
							Value:     aws.Float64(100),
						},
					}, nil).Times(2)
					_, err = r.HandleNonCCSPendingVerification(nullLogger, account, mockAWSClient)
					Expect(account.Status.RegionalServiceQuotas["us-east-1"][awsv1alpha1.RunningStandardInstances].Status).To(Equal(awsv1alpha1.ServiceRequestCompleted))
					Expect(account.Status.RegionalServiceQuotas["us-east-2"][awsv1alpha1.RunningStandardInstances].Status).To(Equal(awsv1alpha1.ServiceRequestCompleted))
					_, err = r.HandleNonCCSPendingVerification(nullLogger, account, mockAWSClient)
					Expect(account.Status.State).To(Equal(AccountReady))
				})
				It("fails the account if a request is denied", func() {
					subClient := mock.NewMockClient(ctrl)
					AssumeRole = func(
						reqLogger logr.Logger,
						awsClientBuilder awsclient.IBuilder,
						currentAcctInstance *awsv1alpha1.Account,
						client client.Client,
						awsSetupClient awsclient.Client,
						region string,
						roleToAssume string,
						ccsRoleID string) (awsclient.Client, *sts.AssumeRoleOutput, error) {
						return subClient, &sts.AssumeRoleOutput{}, nil
					}
					// Reconciliation loop 1
					mockAWSClient.EXPECT().CreateCase(gomock.Any()).Return(&support.CreateCaseOutput{
						CaseId: aws.String("123456"),
					}, nil)
					subClient.EXPECT().DescribeRegions(gomock.Any()).Return(&ec2.DescribeRegionsOutput{
						Regions: []*ec2.Region{
							{
								RegionName: aws.String("us-east-1"),
							},
						},
					}, nil)
					_, err = r.HandleNonCCSPendingVerification(nullLogger, account, mockAWSClient)
					Expect(err).ToNot(HaveOccurred())
					Expect(len(account.Status.RegionalServiceQuotas)).To(Equal(1))
					Expect(len(account.Status.RegionalServiceQuotas["us-east-1"])).To(Equal(1))
					mockAWSClient.EXPECT().DescribeCases(gomock.Any()).Return(&support.DescribeCasesOutput{
						Cases: []*support.CaseDetails{
							{
								CaseId: aws.String("123456"),
								Status: aws.String("resolved"),
							},
						},
					}, nil)
					subClient.EXPECT().ListRequestedServiceQuotaChangeHistoryByQuota(gomock.Any()).Return(&servicequotas.ListRequestedServiceQuotaChangeHistoryByQuotaOutput{
						RequestedQuotas: []*servicequotas.RequestedServiceQuotaChange{
							{
								DesiredValue: aws.Float64(100),
								QuotaCode:    aws.String(string(awsv1alpha1.RunningStandardInstances)),
								ServiceCode:  aws.String(string(awsv1alpha1.EC2ServiceQuota)),
								Status:       aws.String("DENIED"),
							},
						},
					}, nil).Times(1)
					// Have to increase both of our quotas
					subClient.EXPECT().GetServiceQuota(gomock.Any()).Return(&servicequotas.GetServiceQuotaOutput{
						Quota: &servicequotas.ServiceQuota{
							QuotaCode: aws.String(string(awsv1alpha1.RunningStandardInstances)),
							Value:     aws.Float64(0),
						},
					}, nil).Times(1)
					_, err = r.HandleNonCCSPendingVerification(nullLogger, account, mockAWSClient)
					Expect(account.Status.RegionalServiceQuotas["us-east-1"][awsv1alpha1.RunningStandardInstances].Status).To(Equal(awsv1alpha1.ServiceRequestDenied))
					Expect(account.Status.State).To(Equal(AccountFailed))
				})
			})
		})
	})
})
