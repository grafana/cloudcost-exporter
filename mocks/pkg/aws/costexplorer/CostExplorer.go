// Code generated by mockery v2.38.0. DO NOT EDIT.

package costexplorer

import (
	context "context"

	servicecostexplorer "github.com/aws/aws-sdk-go-v2/service/costexplorer"
	mock "github.com/stretchr/testify/mock"
)

// CostExplorer is an autogenerated mock type for the CostExplorer type
type CostExplorer struct {
	mock.Mock
}

type CostExplorer_Expecter struct {
	mock *mock.Mock
}

func (_m *CostExplorer) EXPECT() *CostExplorer_Expecter {
	return &CostExplorer_Expecter{mock: &_m.Mock}
}

// GetCostAndUsage provides a mock function with given fields: ctx, params, optFns
func (_m *CostExplorer) GetCostAndUsage(ctx context.Context, params *servicecostexplorer.GetCostAndUsageInput, optFns ...func(*servicecostexplorer.Options)) (*servicecostexplorer.GetCostAndUsageOutput, error) {
	_va := make([]interface{}, len(optFns))
	for _i := range optFns {
		_va[_i] = optFns[_i]
	}
	var _ca []interface{}
	_ca = append(_ca, ctx, params)
	_ca = append(_ca, _va...)
	ret := _m.Called(_ca...)

	if len(ret) == 0 {
		panic("no return value specified for GetCostAndUsage")
	}

	var r0 *servicecostexplorer.GetCostAndUsageOutput
	var r1 error
	if rf, ok := ret.Get(0).(func(context.Context, *servicecostexplorer.GetCostAndUsageInput, ...func(*servicecostexplorer.Options)) (*servicecostexplorer.GetCostAndUsageOutput, error)); ok {
		return rf(ctx, params, optFns...)
	}
	if rf, ok := ret.Get(0).(func(context.Context, *servicecostexplorer.GetCostAndUsageInput, ...func(*servicecostexplorer.Options)) *servicecostexplorer.GetCostAndUsageOutput); ok {
		r0 = rf(ctx, params, optFns...)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(*servicecostexplorer.GetCostAndUsageOutput)
		}
	}

	if rf, ok := ret.Get(1).(func(context.Context, *servicecostexplorer.GetCostAndUsageInput, ...func(*servicecostexplorer.Options)) error); ok {
		r1 = rf(ctx, params, optFns...)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

// CostExplorer_GetCostAndUsage_Call is a *mock.Call that shadows Run/Return methods with type explicit version for method 'GetCostAndUsage'
type CostExplorer_GetCostAndUsage_Call struct {
	*mock.Call
}

// GetCostAndUsage is a helper method to define mock.On call
//   - ctx context.Context
//   - params *servicecostexplorer.GetCostAndUsageInput
//   - optFns ...func(*servicecostexplorer.Options)
func (_e *CostExplorer_Expecter) GetCostAndUsage(ctx interface{}, params interface{}, optFns ...interface{}) *CostExplorer_GetCostAndUsage_Call {
	return &CostExplorer_GetCostAndUsage_Call{Call: _e.mock.On("GetCostAndUsage",
		append([]interface{}{ctx, params}, optFns...)...)}
}

func (_c *CostExplorer_GetCostAndUsage_Call) Run(run func(ctx context.Context, params *servicecostexplorer.GetCostAndUsageInput, optFns ...func(*servicecostexplorer.Options))) *CostExplorer_GetCostAndUsage_Call {
	_c.Call.Run(func(args mock.Arguments) {
		variadicArgs := make([]func(*servicecostexplorer.Options), len(args)-2)
		for i, a := range args[2:] {
			if a != nil {
				variadicArgs[i] = a.(func(*servicecostexplorer.Options))
			}
		}
		run(args[0].(context.Context), args[1].(*servicecostexplorer.GetCostAndUsageInput), variadicArgs...)
	})
	return _c
}

func (_c *CostExplorer_GetCostAndUsage_Call) Return(_a0 *servicecostexplorer.GetCostAndUsageOutput, _a1 error) *CostExplorer_GetCostAndUsage_Call {
	_c.Call.Return(_a0, _a1)
	return _c
}

func (_c *CostExplorer_GetCostAndUsage_Call) RunAndReturn(run func(context.Context, *servicecostexplorer.GetCostAndUsageInput, ...func(*servicecostexplorer.Options)) (*servicecostexplorer.GetCostAndUsageOutput, error)) *CostExplorer_GetCostAndUsage_Call {
	_c.Call.Return(run)
	return _c
}

// NewCostExplorer creates a new instance of CostExplorer. It also registers a testing interface on the mock and a cleanup function to assert the mocks expectations.
// The first argument is typically a *testing.T value.
func NewCostExplorer(t interface {
	mock.TestingT
	Cleanup(func())
}) *CostExplorer {
	mock := &CostExplorer{}
	mock.Mock.Test(t)

	t.Cleanup(func() { mock.AssertExpectations(t) })

	return mock
}