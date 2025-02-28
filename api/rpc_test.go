package api

import (
	"context"
	"encoding/json"
	"reflect"
	"strconv"
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cristalhq/jwt"
	"github.com/golang/mock/gomock"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx"

	"github.com/celestiaorg/celestia-node/api/rpc"
	"github.com/celestiaorg/celestia-node/api/rpc/client"
	"github.com/celestiaorg/celestia-node/api/rpc/perms"
	daspkg "github.com/celestiaorg/celestia-node/das"
	headerpkg "github.com/celestiaorg/celestia-node/header"
	"github.com/celestiaorg/celestia-node/nodebuilder"
	"github.com/celestiaorg/celestia-node/nodebuilder/das"
	dasMock "github.com/celestiaorg/celestia-node/nodebuilder/das/mocks"
	"github.com/celestiaorg/celestia-node/nodebuilder/fraud"
	fraudMock "github.com/celestiaorg/celestia-node/nodebuilder/fraud/mocks"
	"github.com/celestiaorg/celestia-node/nodebuilder/header"
	headerMock "github.com/celestiaorg/celestia-node/nodebuilder/header/mocks"
	"github.com/celestiaorg/celestia-node/nodebuilder/node"
	nodeMock "github.com/celestiaorg/celestia-node/nodebuilder/node/mocks"
	"github.com/celestiaorg/celestia-node/nodebuilder/p2p"
	p2pMock "github.com/celestiaorg/celestia-node/nodebuilder/p2p/mocks"
	"github.com/celestiaorg/celestia-node/nodebuilder/share"
	shareMock "github.com/celestiaorg/celestia-node/nodebuilder/share/mocks"
	statemod "github.com/celestiaorg/celestia-node/nodebuilder/state"
	stateMock "github.com/celestiaorg/celestia-node/nodebuilder/state/mocks"
	"github.com/celestiaorg/celestia-node/state"
)

func TestRPCCallsUnderlyingNode(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	nd, server := setupNodeWithModifiedRPC(t)
	url := nd.RPCServer.ListenAddr()
	// we need to run this a few times to prevent the race where the server is not yet started
	var (
		rpcClient *client.Client
		err       error
	)
	for i := 0; i < 3; i++ {
		time.Sleep(time.Second * 1)
		rpcClient, err = client.NewPublicClient(ctx, "http://"+url)
		if err == nil {
			t.Cleanup(rpcClient.Close)
			break
		}
	}
	require.NotNil(t, rpcClient)
	require.NoError(t, err)

	expectedBalance := &state.Balance{
		Amount: sdk.NewInt(100),
		Denom:  "utia",
	}

	server.State.EXPECT().Balance(gomock.Any()).Return(expectedBalance, nil)

	balance, err := rpcClient.State.Balance(ctx)
	require.NoError(t, err)
	require.Equal(t, expectedBalance, balance)
}

// api contains all modules that are made available as the node's
// public API surface (except for the `node` module as the node
// module contains a method `Info` that is also contained in the
// p2p module).
type api interface {
	fraud.Module
	header.Module
	statemod.Module
	share.Module
	das.Module
	p2p.Module
}

func TestModulesImplementFullAPI(t *testing.T) {
	api := reflect.TypeOf(new(api)).Elem()
	nodeapi := reflect.TypeOf(new(node.Module)).Elem() // TODO @renaynay: explain
	client := reflect.TypeOf(new(client.Client)).Elem()
	for i := 0; i < client.NumField(); i++ {
		module := client.Field(i)
		switch module.Name {
		case "closer":
			// the "closers" field is not an actual module
			continue
		case "Node":
			// node module contains a duplicate method to the p2p module
			// and must be tested separately.
			internal, ok := module.Type.FieldByName("Internal")
			require.True(t, ok, "module %s's API does not have an Internal field", module.Name)
			for j := 0; j < internal.Type.NumField(); j++ {
				impl := internal.Type.Field(j)
				method, _ := nodeapi.MethodByName(impl.Name)
				require.Equal(t, method.Type, impl.Type, "method %s does not match", impl.Name)
			}
		default:
			internal, ok := module.Type.FieldByName("Internal")
			require.True(t, ok, "module %s's API does not have an Internal field", module.Name)
			for j := 0; j < internal.Type.NumField(); j++ {
				impl := internal.Type.Field(j)
				method, _ := api.MethodByName(impl.Name)
				require.Equal(t, method.Type, impl.Type, "method %s does not match", impl.Name)
			}
		}
	}
}

func TestAuthedRPC(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	// generate dummy signer and sign admin perms token with it
	signer, err := jwt.NewHS256(make([]byte, 32))
	require.NoError(t, err)

	nd, server := setupNodeWithAuthedRPC(t, signer)
	url := nd.RPCServer.ListenAddr()

	// create permissioned tokens
	publicToken, err := perms.NewTokenWithPerms(signer, perms.DefaultPerms)
	require.NoError(t, err)
	readToken, err := perms.NewTokenWithPerms(signer, perms.ReadPerms)
	require.NoError(t, err)
	rwToken, err := perms.NewTokenWithPerms(signer, perms.ReadWritePerms)
	require.NoError(t, err)
	adminToken, err := perms.NewTokenWithPerms(signer, perms.AllPerms)
	require.NoError(t, err)

	var tests = []struct {
		perm  int
		token string
	}{
		{perm: 1, token: string(publicToken)}, // public
		{perm: 2, token: string(readToken)},   // read
		{perm: 3, token: string(rwToken)},     // RW
		{perm: 4, token: string(adminToken)},  // admin
	}

	for i, tt := range tests {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			// we need to run this a few times to prevent the race where the server is not yet started
			var rpcClient *client.Client
			require.NoError(t, err)
			for i := 0; i < 3; i++ {
				time.Sleep(time.Second * 1)
				rpcClient, err = client.NewClient(ctx, "http://"+url, tt.token)
				if err == nil {
					break
				}
			}
			require.NotNil(t, rpcClient)
			require.NoError(t, err)

			// 1. Test method with public permissions
			server.Header.EXPECT().NetworkHead(gomock.Any()).Return(new(headerpkg.ExtendedHeader), nil)
			got, err := rpcClient.Header.NetworkHead(ctx)
			require.NoError(t, err)
			require.NotNil(t, got)

			// 2. Test method with read-level permissions
			expected := daspkg.SamplingStats{
				SampledChainHead: 100,
				CatchupHead:      100,
				NetworkHead:      1000,
				Failed:           nil,
				Workers:          nil,
				Concurrency:      0,
				CatchUpDone:      true,
				IsRunning:        false,
			}
			if tt.perm > 1 {
				server.Das.EXPECT().SamplingStats(gomock.Any()).Return(expected, nil)
				stats, err := rpcClient.DAS.SamplingStats(ctx)
				require.NoError(t, err)
				require.Equal(t, expected, stats)
			} else {
				_, err := rpcClient.DAS.SamplingStats(ctx)
				require.Error(t, err)
				require.ErrorContains(t, err, "missing permission")
			}

			// 3. Test method with write-level permissions
			expectedResp := &state.TxResponse{}
			if tt.perm > 2 {
				server.State.EXPECT().SubmitTx(gomock.Any(), gomock.Any()).Return(expectedResp, nil)
				txResp, err := rpcClient.State.SubmitTx(ctx, []byte{})
				require.NoError(t, err)
				require.Equal(t, expectedResp, txResp)
			} else {
				_, err := rpcClient.State.SubmitTx(ctx, []byte{})
				require.Error(t, err)
				require.ErrorContains(t, err, "missing permission")
			}

			// 4. Test method with admin-level permissions
			expectedReachability := network.Reachability(3)
			if tt.perm > 3 {
				server.P2P.EXPECT().NATStatus(gomock.Any()).Return(expectedReachability, nil)
				natstatus, err := rpcClient.P2P.NATStatus(ctx)
				require.NoError(t, err)
				require.Equal(t, expectedReachability, natstatus)
			} else {
				_, err := rpcClient.P2P.NATStatus(ctx)
				require.Error(t, err)
				require.ErrorContains(t, err, "missing permission")
			}

			rpcClient.Close()
		})
	}
}

// TestPublicClient tests that the public rpc client can only
// access public methods.
func TestPublicClient(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	// generate dummy signer and sign admin perms token with it
	signer, err := jwt.NewHS256(make([]byte, 32))
	require.NoError(t, err)

	nd, server := setupNodeWithAuthedRPC(t, signer)
	url := nd.RPCServer.ListenAddr()

	// we need to run this a few times to prevent the race where the server is not yet started
	var rpcClient *client.Client
	require.NoError(t, err)
	for i := 0; i < 3; i++ {
		time.Sleep(time.Second * 1)
		rpcClient, err = client.NewPublicClient(ctx, "http://"+url)
		if err == nil {
			t.Cleanup(rpcClient.Close)
			break
		}
	}
	require.NotNil(t, rpcClient)
	require.NoError(t, err)

	// 1. Test method with public permissions
	server.Header.EXPECT().NetworkHead(gomock.Any()).Return(new(headerpkg.ExtendedHeader), nil)
	got, err := rpcClient.Header.NetworkHead(ctx)
	require.NoError(t, err)
	require.NotNil(t, got)

	// 2. Test method with read-level permissions
	_, err = rpcClient.DAS.SamplingStats(ctx)
	require.Error(t, err)
	require.ErrorContains(t, err, "missing permission")

	// 3. Test method with write-level permissions
	_, err = rpcClient.State.SubmitTx(ctx, []byte{})
	require.Error(t, err)
	require.ErrorContains(t, err, "missing permission")

	// 4. Test method with admin-level permissions
	_, err = rpcClient.P2P.NATStatus(ctx)
	require.Error(t, err)
	require.ErrorContains(t, err, "missing permission")
}

func TestAllReturnValuesAreMarshalable(t *testing.T) {
	ra := reflect.TypeOf(new(api)).Elem()
	for i := 0; i < ra.NumMethod(); i++ {
		m := ra.Method(i)
		for j := 0; j < m.Type.NumOut(); j++ {
			implementsMarshaler(t, m.Type.Out(j))
		}
	}
	// NOTE: see comment above api interface definition.
	na := reflect.TypeOf(new(node.Module)).Elem()
	for i := 0; i < na.NumMethod(); i++ {
		m := na.Method(i)
		for j := 0; j < m.Type.NumOut(); j++ {
			implementsMarshaler(t, m.Type.Out(j))
		}
	}
}

func implementsMarshaler(t *testing.T, typ reflect.Type) {
	// the passed type may already implement json.Marshaler and we don't need to go deeper
	if typ.Implements(reflect.TypeOf(new(json.Marshaler)).Elem()) {
		return
	}

	switch typ.Kind() {
	case reflect.Struct:
		// a user defined struct could implement json.Marshaler on the pointer receiver, so check there
		// first. note that the "non-pointer" receiver is checked before the switch.
		pointerType := reflect.TypeOf(reflect.New(typ).Elem().Addr().Interface())
		if pointerType.Implements(reflect.TypeOf(new(json.Marshaler)).Elem()) {
			return
		}
		// struct doesn't implement the interface itself, check all individual fields
		reflect.New(typ).Pointer()
		for i := 0; i < typ.NumField(); i++ {
			implementsMarshaler(t, typ.Field(i).Type)
		}
		return
	case reflect.Map:
		implementsMarshaler(t, typ.Elem())
		implementsMarshaler(t, typ.Key())
	case reflect.Ptr:
		fallthrough
	case reflect.Array:
		fallthrough
	case reflect.Slice:
		fallthrough
	case reflect.Chan:
		implementsMarshaler(t, typ.Elem())
	case reflect.Interface:
		if typ != reflect.TypeOf(new(interface{})).Elem() && typ != reflect.TypeOf(new(error)).Elem() {
			require.True(
				t,
				typ.Implements(reflect.TypeOf(new(json.Marshaler)).Elem()),
				"type %s does not implement json.Marshaler", typ.String(),
			)
		}
	default:
		return
	}

}

func setupNodeWithModifiedRPC(t *testing.T) (*nodebuilder.Node, *mockAPI) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	ctrl := gomock.NewController(t)

	mockAPI := &mockAPI{
		stateMock.NewMockModule(ctrl),
		shareMock.NewMockModule(ctrl),
		fraudMock.NewMockModule(ctrl),
		headerMock.NewMockModule(ctrl),
		dasMock.NewMockModule(ctrl),
		p2pMock.NewMockModule(ctrl),
		nodeMock.NewMockModule(ctrl),
	}

	// given the behavior of fx.Invoke, this invoke will be called last as it is added at the root
	// level module. For further information, check the documentation on fx.Invoke.
	invokeRPC := fx.Invoke(func(srv *rpc.Server) {
		srv.RegisterService("state", mockAPI.State)
		srv.RegisterService("share", mockAPI.Share)
		srv.RegisterService("fraud", mockAPI.Fraud)
		srv.RegisterService("header", mockAPI.Header)
		srv.RegisterService("das", mockAPI.Das)
		srv.RegisterService("p2p", mockAPI.P2P)
		srv.RegisterService("node", mockAPI.Node)
	})
	nd := nodebuilder.TestNode(t, node.Full, invokeRPC)
	// start node
	err := nd.Start(ctx)
	require.NoError(t, err)
	t.Cleanup(func() {
		err = nd.Stop(ctx)
		require.NoError(t, err)
	})
	return nd, mockAPI
}

// setupNodeWithAuthedRPC sets up a node and overrides its JWT
// signer with the given signer.
func setupNodeWithAuthedRPC(t *testing.T, auth jwt.Signer) (*nodebuilder.Node, *mockAPI) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	ctrl := gomock.NewController(t)

	mockAPI := &mockAPI{
		stateMock.NewMockModule(ctrl),
		shareMock.NewMockModule(ctrl),
		fraudMock.NewMockModule(ctrl),
		headerMock.NewMockModule(ctrl),
		dasMock.NewMockModule(ctrl),
		p2pMock.NewMockModule(ctrl),
		nodeMock.NewMockModule(ctrl),
	}

	// given the behavior of fx.Invoke, this invoke will be called last as it is added at the root
	// level module. For further information, check the documentation on fx.Invoke.
	invokeRPC := fx.Invoke(func(srv *rpc.Server) {
		srv.RegisterAuthedService("state", mockAPI.State, &statemod.API{})
		srv.RegisterAuthedService("share", mockAPI.Share, &share.API{})
		srv.RegisterAuthedService("fraud", mockAPI.Fraud, &fraud.API{})
		srv.RegisterAuthedService("header", mockAPI.Header, &header.API{})
		srv.RegisterAuthedService("das", mockAPI.Das, &das.API{})
		srv.RegisterAuthedService("p2p", mockAPI.P2P, &p2p.API{})
		srv.RegisterAuthedService("node", mockAPI.Node, &node.API{})
	})
	// fx.Replace does not work here, but fx.Decorate does
	nd := nodebuilder.TestNode(t, node.Full, invokeRPC, fx.Decorate(func() (jwt.Signer, error) {
		return auth, nil
	}))
	// start node
	err := nd.Start(ctx)
	require.NoError(t, err)
	t.Cleanup(func() {
		err = nd.Stop(ctx)
		require.NoError(t, err)
	})
	return nd, mockAPI
}

type mockAPI struct {
	State  *stateMock.MockModule
	Share  *shareMock.MockModule
	Fraud  *fraudMock.MockModule
	Header *headerMock.MockModule
	Das    *dasMock.MockModule
	P2P    *p2pMock.MockModule
	Node   *nodeMock.MockModule
}
