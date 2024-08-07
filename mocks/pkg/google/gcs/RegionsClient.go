// Code generated by mockery v2.43.2. DO NOT EDIT.

package gcs

import (
	compute "cloud.google.com/go/compute/apiv1"
	computepb "cloud.google.com/go/compute/apiv1/computepb"

	context "context"

	gax "github.com/googleapis/gax-go/v2"

	mock "github.com/stretchr/testify/mock"
)

// RegionsClient is an autogenerated mock type for the RegionsClient type
type RegionsClient struct {
	mock.Mock
}

type RegionsClient_Expecter struct {
	mock *mock.Mock
}

func (_m *RegionsClient) EXPECT() *RegionsClient_Expecter {
	return &RegionsClient_Expecter{mock: &_m.Mock}
}

// List provides a mock function with given fields: ctx, req, opts
func (_m *RegionsClient) List(ctx context.Context, req *computepb.ListRegionsRequest, opts ...gax.CallOption) *compute.RegionIterator {
	_va := make([]interface{}, len(opts))
	for _i := range opts {
		_va[_i] = opts[_i]
	}
	var _ca []interface{}
	_ca = append(_ca, ctx, req)
	_ca = append(_ca, _va...)
	ret := _m.Called(_ca...)

	if len(ret) == 0 {
		panic("no return value specified for List")
	}

	var r0 *compute.RegionIterator
	if rf, ok := ret.Get(0).(func(context.Context, *computepb.ListRegionsRequest, ...gax.CallOption) *compute.RegionIterator); ok {
		r0 = rf(ctx, req, opts...)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(*compute.RegionIterator)
		}
	}

	return r0
}

// RegionsClient_List_Call is a *mock.Call that shadows Run/Return methods with type explicit version for method 'List'
type RegionsClient_List_Call struct {
	*mock.Call
}

// List is a helper method to define mock.On call
//   - ctx context.Context
//   - req *computepb.ListRegionsRequest
//   - opts ...gax.CallOption
func (_e *RegionsClient_Expecter) List(ctx interface{}, req interface{}, opts ...interface{}) *RegionsClient_List_Call {
	return &RegionsClient_List_Call{Call: _e.mock.On("List",
		append([]interface{}{ctx, req}, opts...)...)}
}

func (_c *RegionsClient_List_Call) Run(run func(ctx context.Context, req *computepb.ListRegionsRequest, opts ...gax.CallOption)) *RegionsClient_List_Call {
	_c.Call.Run(func(args mock.Arguments) {
		variadicArgs := make([]gax.CallOption, len(args)-2)
		for i, a := range args[2:] {
			if a != nil {
				variadicArgs[i] = a.(gax.CallOption)
			}
		}
		run(args[0].(context.Context), args[1].(*computepb.ListRegionsRequest), variadicArgs...)
	})
	return _c
}

func (_c *RegionsClient_List_Call) Return(_a0 *compute.RegionIterator) *RegionsClient_List_Call {
	_c.Call.Return(_a0)
	return _c
}

func (_c *RegionsClient_List_Call) RunAndReturn(run func(context.Context, *computepb.ListRegionsRequest, ...gax.CallOption) *compute.RegionIterator) *RegionsClient_List_Call {
	_c.Call.Return(run)
	return _c
}

// NewRegionsClient creates a new instance of RegionsClient. It also registers a testing interface on the mock and a cleanup function to assert the mocks expectations.
// The first argument is typically a *testing.T value.
func NewRegionsClient(t interface {
	mock.TestingT
	Cleanup(func())
}) *RegionsClient {
	mock := &RegionsClient{}
	mock.Mock.Test(t)

	t.Cleanup(func() { mock.AssertExpectations(t) })

	return mock
}
