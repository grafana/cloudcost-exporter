// Code generated by mockery v2.43.2. DO NOT EDIT.

package gcs

import (
	context "context"

	storage "cloud.google.com/go/storage"
	mock "github.com/stretchr/testify/mock"
)

// StorageClientInterface is an autogenerated mock type for the StorageClientInterface type
type StorageClientInterface struct {
	mock.Mock
}

type StorageClientInterface_Expecter struct {
	mock *mock.Mock
}

func (_m *StorageClientInterface) EXPECT() *StorageClientInterface_Expecter {
	return &StorageClientInterface_Expecter{mock: &_m.Mock}
}

// Buckets provides a mock function with given fields: ctx, projectID
func (_m *StorageClientInterface) Buckets(ctx context.Context, projectID string) *storage.BucketIterator {
	ret := _m.Called(ctx, projectID)

	if len(ret) == 0 {
		panic("no return value specified for Buckets")
	}

	var r0 *storage.BucketIterator
	if rf, ok := ret.Get(0).(func(context.Context, string) *storage.BucketIterator); ok {
		r0 = rf(ctx, projectID)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(*storage.BucketIterator)
		}
	}

	return r0
}

// StorageClientInterface_Buckets_Call is a *mock.Call that shadows Run/Return methods with type explicit version for method 'Buckets'
type StorageClientInterface_Buckets_Call struct {
	*mock.Call
}

// Buckets is a helper method to define mock.On call
//   - ctx context.Context
//   - projectID string
func (_e *StorageClientInterface_Expecter) Buckets(ctx interface{}, projectID interface{}) *StorageClientInterface_Buckets_Call {
	return &StorageClientInterface_Buckets_Call{Call: _e.mock.On("Buckets", ctx, projectID)}
}

func (_c *StorageClientInterface_Buckets_Call) Run(run func(ctx context.Context, projectID string)) *StorageClientInterface_Buckets_Call {
	_c.Call.Run(func(args mock.Arguments) {
		run(args[0].(context.Context), args[1].(string))
	})
	return _c
}

func (_c *StorageClientInterface_Buckets_Call) Return(_a0 *storage.BucketIterator) *StorageClientInterface_Buckets_Call {
	_c.Call.Return(_a0)
	return _c
}

func (_c *StorageClientInterface_Buckets_Call) RunAndReturn(run func(context.Context, string) *storage.BucketIterator) *StorageClientInterface_Buckets_Call {
	_c.Call.Return(run)
	return _c
}

// NewStorageClientInterface creates a new instance of StorageClientInterface. It also registers a testing interface on the mock and a cleanup function to assert the mocks expectations.
// The first argument is typically a *testing.T value.
func NewStorageClientInterface(t interface {
	mock.TestingT
	Cleanup(func())
}) *StorageClientInterface {
	mock := &StorageClientInterface{}
	mock.Mock.Test(t)

	t.Cleanup(func() { mock.AssertExpectations(t) })

	return mock
}
