// Code generated by MockGen. DO NOT EDIT.
// Source: pkg/provider/provider.go
//
// Generated by this command:
//
//	mockgen -source=pkg/provider/provider.go -destination pkg/provider/mocks/provider.go
//
// Package mock_provider is a generated GoMock package.
package mock_provider

import (
	reflect "reflect"

	provider "github.com/grafana/cloudcost-exporter/pkg/provider"
	prometheus "github.com/prometheus/client_golang/prometheus"
	io_prometheus_client "github.com/prometheus/client_model/go"
	gomock "go.uber.org/mock/gomock"
)

// MockRegistry is a mock of Registry interface.
type MockRegistry struct {
	ctrl     *gomock.Controller
	recorder *MockRegistryMockRecorder
}

// MockRegistryMockRecorder is the mock recorder for MockRegistry.
type MockRegistryMockRecorder struct {
	mock *MockRegistry
}

// NewMockRegistry creates a new mock instance.
func NewMockRegistry(ctrl *gomock.Controller) *MockRegistry {
	mock := &MockRegistry{ctrl: ctrl}
	mock.recorder = &MockRegistryMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockRegistry) EXPECT() *MockRegistryMockRecorder {
	return m.recorder
}

// Collect mocks base method.
func (m *MockRegistry) Collect(arg0 chan<- prometheus.Metric) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "Collect", arg0)
}

// Collect indicates an expected call of Collect.
func (mr *MockRegistryMockRecorder) Collect(arg0 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Collect", reflect.TypeOf((*MockRegistry)(nil).Collect), arg0)
}

// Describe mocks base method.
func (m *MockRegistry) Describe(arg0 chan<- *prometheus.Desc) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "Describe", arg0)
}

// Describe indicates an expected call of Describe.
func (mr *MockRegistryMockRecorder) Describe(arg0 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Describe", reflect.TypeOf((*MockRegistry)(nil).Describe), arg0)
}

// Gather mocks base method.
func (m *MockRegistry) Gather() ([]*io_prometheus_client.MetricFamily, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Gather")
	ret0, _ := ret[0].([]*io_prometheus_client.MetricFamily)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Gather indicates an expected call of Gather.
func (mr *MockRegistryMockRecorder) Gather() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Gather", reflect.TypeOf((*MockRegistry)(nil).Gather))
}

// MustRegister mocks base method.
func (m *MockRegistry) MustRegister(arg0 ...prometheus.Collector) {
	m.ctrl.T.Helper()
	varargs := []any{}
	for _, a := range arg0 {
		varargs = append(varargs, a)
	}
	m.ctrl.Call(m, "MustRegister", varargs...)
}

// MustRegister indicates an expected call of MustRegister.
func (mr *MockRegistryMockRecorder) MustRegister(arg0 ...any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "MustRegister", reflect.TypeOf((*MockRegistry)(nil).MustRegister), arg0...)
}

// Register mocks base method.
func (m *MockRegistry) Register(arg0 prometheus.Collector) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Register", arg0)
	ret0, _ := ret[0].(error)
	return ret0
}

// Register indicates an expected call of Register.
func (mr *MockRegistryMockRecorder) Register(arg0 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Register", reflect.TypeOf((*MockRegistry)(nil).Register), arg0)
}

// Unregister mocks base method.
func (m *MockRegistry) Unregister(arg0 prometheus.Collector) bool {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Unregister", arg0)
	ret0, _ := ret[0].(bool)
	return ret0
}

// Unregister indicates an expected call of Unregister.
func (mr *MockRegistryMockRecorder) Unregister(arg0 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Unregister", reflect.TypeOf((*MockRegistry)(nil).Unregister), arg0)
}

// MockCollector is a mock of Collector interface.
type MockCollector struct {
	ctrl     *gomock.Controller
	recorder *MockCollectorMockRecorder
}

// MockCollectorMockRecorder is the mock recorder for MockCollector.
type MockCollectorMockRecorder struct {
	mock *MockCollector
}

// NewMockCollector creates a new mock instance.
func NewMockCollector(ctrl *gomock.Controller) *MockCollector {
	mock := &MockCollector{ctrl: ctrl}
	mock.recorder = &MockCollectorMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockCollector) EXPECT() *MockCollectorMockRecorder {
	return m.recorder
}

// Collect mocks base method.
func (m *MockCollector) Collect(arg0 chan<- prometheus.Metric) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Collect", arg0)
	ret0, _ := ret[0].(error)
	return ret0
}

// Collect indicates an expected call of Collect.
func (mr *MockCollectorMockRecorder) Collect(arg0 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Collect", reflect.TypeOf((*MockCollector)(nil).Collect), arg0)
}

// CollectMetrics mocks base method.
func (m *MockCollector) CollectMetrics(arg0 chan<- prometheus.Metric) float64 {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "CollectMetrics", arg0)
	ret0, _ := ret[0].(float64)
	return ret0
}

// CollectMetrics indicates an expected call of CollectMetrics.
func (mr *MockCollectorMockRecorder) CollectMetrics(arg0 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "CollectMetrics", reflect.TypeOf((*MockCollector)(nil).CollectMetrics), arg0)
}

// Describe mocks base method.
func (m *MockCollector) Describe(arg0 chan<- *prometheus.Desc) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Describe", arg0)
	ret0, _ := ret[0].(error)
	return ret0
}

// Describe indicates an expected call of Describe.
func (mr *MockCollectorMockRecorder) Describe(arg0 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Describe", reflect.TypeOf((*MockCollector)(nil).Describe), arg0)
}

// Name mocks base method.
func (m *MockCollector) Name() string {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Name")
	ret0, _ := ret[0].(string)
	return ret0
}

// Name indicates an expected call of Name.
func (mr *MockCollectorMockRecorder) Name() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Name", reflect.TypeOf((*MockCollector)(nil).Name))
}

// Register mocks base method.
func (m *MockCollector) Register(r provider.Registry) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Register", r)
	ret0, _ := ret[0].(error)
	return ret0
}

// Register indicates an expected call of Register.
func (mr *MockCollectorMockRecorder) Register(r any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Register", reflect.TypeOf((*MockCollector)(nil).Register), r)
}

// MockProvider is a mock of Provider interface.
type MockProvider struct {
	ctrl     *gomock.Controller
	recorder *MockProviderMockRecorder
}

// MockProviderMockRecorder is the mock recorder for MockProvider.
type MockProviderMockRecorder struct {
	mock *MockProvider
}

// NewMockProvider creates a new mock instance.
func NewMockProvider(ctrl *gomock.Controller) *MockProvider {
	mock := &MockProvider{ctrl: ctrl}
	mock.recorder = &MockProviderMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockProvider) EXPECT() *MockProviderMockRecorder {
	return m.recorder
}

// Collect mocks base method.
func (m *MockProvider) Collect(arg0 chan<- prometheus.Metric) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "Collect", arg0)
}

// Collect indicates an expected call of Collect.
func (mr *MockProviderMockRecorder) Collect(arg0 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Collect", reflect.TypeOf((*MockProvider)(nil).Collect), arg0)
}

// Describe mocks base method.
func (m *MockProvider) Describe(arg0 chan<- *prometheus.Desc) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "Describe", arg0)
}

// Describe indicates an expected call of Describe.
func (mr *MockProviderMockRecorder) Describe(arg0 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Describe", reflect.TypeOf((*MockProvider)(nil).Describe), arg0)
}

// RegisterCollectors mocks base method.
func (m *MockProvider) RegisterCollectors(r provider.Registry) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "RegisterCollectors", r)
	ret0, _ := ret[0].(error)
	return ret0
}

// RegisterCollectors indicates an expected call of RegisterCollectors.
func (mr *MockProviderMockRecorder) RegisterCollectors(r any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "RegisterCollectors", reflect.TypeOf((*MockProvider)(nil).RegisterCollectors), r)
}
