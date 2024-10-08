// Code generated by mockery v2.43.2. DO NOT EDIT.

package provider

import (
	prometheus "github.com/prometheus/client_golang/prometheus"
	mock "github.com/stretchr/testify/mock"
)

// Provider is an autogenerated mock type for the Provider type
type Provider struct {
	mock.Mock
}

type Provider_Expecter struct {
	mock *mock.Mock
}

func (_m *Provider) EXPECT() *Provider_Expecter {
	return &Provider_Expecter{mock: &_m.Mock}
}

// Collect provides a mock function with given fields: _a0
func (_m *Provider) Collect(_a0 chan<- prometheus.Metric) {
	_m.Called(_a0)
}

// Provider_Collect_Call is a *mock.Call that shadows Run/Return methods with type explicit version for method 'Collect'
type Provider_Collect_Call struct {
	*mock.Call
}

// Collect is a helper method to define mock.On call
//   - _a0 chan<- prometheus.Metric
func (_e *Provider_Expecter) Collect(_a0 interface{}) *Provider_Collect_Call {
	return &Provider_Collect_Call{Call: _e.mock.On("Collect", _a0)}
}

func (_c *Provider_Collect_Call) Run(run func(_a0 chan<- prometheus.Metric)) *Provider_Collect_Call {
	_c.Call.Run(func(args mock.Arguments) {
		run(args[0].(chan<- prometheus.Metric))
	})
	return _c
}

func (_c *Provider_Collect_Call) Return() *Provider_Collect_Call {
	_c.Call.Return()
	return _c
}

func (_c *Provider_Collect_Call) RunAndReturn(run func(chan<- prometheus.Metric)) *Provider_Collect_Call {
	_c.Call.Return(run)
	return _c
}

// Describe provides a mock function with given fields: _a0
func (_m *Provider) Describe(_a0 chan<- *prometheus.Desc) {
	_m.Called(_a0)
}

// Provider_Describe_Call is a *mock.Call that shadows Run/Return methods with type explicit version for method 'Describe'
type Provider_Describe_Call struct {
	*mock.Call
}

// Describe is a helper method to define mock.On call
//   - _a0 chan<- *prometheus.Desc
func (_e *Provider_Expecter) Describe(_a0 interface{}) *Provider_Describe_Call {
	return &Provider_Describe_Call{Call: _e.mock.On("Describe", _a0)}
}

func (_c *Provider_Describe_Call) Run(run func(_a0 chan<- *prometheus.Desc)) *Provider_Describe_Call {
	_c.Call.Run(func(args mock.Arguments) {
		run(args[0].(chan<- *prometheus.Desc))
	})
	return _c
}

func (_c *Provider_Describe_Call) Return() *Provider_Describe_Call {
	_c.Call.Return()
	return _c
}

func (_c *Provider_Describe_Call) RunAndReturn(run func(chan<- *prometheus.Desc)) *Provider_Describe_Call {
	_c.Call.Return(run)
	return _c
}

// RegisterCollectors provides a mock function with given fields: r
func (_m *Provider) RegisterCollectors(r Registry) error {
	ret := _m.Called(r)

	if len(ret) == 0 {
		panic("no return value specified for RegisterCollectors")
	}

	var r0 error
	if rf, ok := ret.Get(0).(func(Registry) error); ok {
		r0 = rf(r)
	} else {
		r0 = ret.Error(0)
	}

	return r0
}

// Provider_RegisterCollectors_Call is a *mock.Call that shadows Run/Return methods with type explicit version for method 'RegisterCollectors'
type Provider_RegisterCollectors_Call struct {
	*mock.Call
}

// RegisterCollectors is a helper method to define mock.On call
//   - r Registry
func (_e *Provider_Expecter) RegisterCollectors(r interface{}) *Provider_RegisterCollectors_Call {
	return &Provider_RegisterCollectors_Call{Call: _e.mock.On("RegisterCollectors", r)}
}

func (_c *Provider_RegisterCollectors_Call) Run(run func(r Registry)) *Provider_RegisterCollectors_Call {
	_c.Call.Run(func(args mock.Arguments) {
		run(args[0].(Registry))
	})
	return _c
}

func (_c *Provider_RegisterCollectors_Call) Return(_a0 error) *Provider_RegisterCollectors_Call {
	_c.Call.Return(_a0)
	return _c
}

func (_c *Provider_RegisterCollectors_Call) RunAndReturn(run func(Registry) error) *Provider_RegisterCollectors_Call {
	_c.Call.Return(run)
	return _c
}

// NewProvider creates a new instance of Provider. It also registers a testing interface on the mock and a cleanup function to assert the mocks expectations.
// The first argument is typically a *testing.T value.
func NewProvider(t interface {
	mock.TestingT
	Cleanup(func())
}) *Provider {
	mock := &Provider{}
	mock.Mock.Test(t)

	t.Cleanup(func() { mock.AssertExpectations(t) })

	return mock
}
