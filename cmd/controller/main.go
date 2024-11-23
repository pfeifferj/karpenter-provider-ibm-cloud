/*
Copyright 2024 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
    "context"
    "fmt"
    "os"

    "sigs.k8s.io/controller-runtime/pkg/client"
    "sigs.k8s.io/controller-runtime/pkg/client/config"
    "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
    "sigs.k8s.io/controller-runtime/pkg/manager"
    "sigs.k8s.io/controller-runtime/pkg/manager/signals"
    "sigs.k8s.io/controller-runtime/pkg/reconcile"
    "sigs.k8s.io/controller-runtime/pkg/builder"
    v1 "sigs.k8s.io/karpenter/pkg/apis/v1"
    ibmcloud "github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/cloudprovider"
    "github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/providers/instance"
    "github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/providers/instancetype"
    "sigs.k8s.io/karpenter/pkg/events"
)

type IBMCloudReconciler struct {
    client.Client
    CloudProvider *ibmcloud.CloudProvider 
}

func NewIBMCloudReconciler(kubeClient client.Client, provider *ibmcloud.CloudProvider) *IBMCloudReconciler {
    return &IBMCloudReconciler{
        Client:        kubeClient,
        CloudProvider: provider,
    }
}

func (r *IBMCloudReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
    var nodeClaim v1.NodeClaim
    if err := r.Get(ctx, req.NamespacedName, &nodeClaim); err != nil {
        // handle not found error
        return reconcile.Result{}, client.IgnoreNotFound(err)
    }

    // Check if the NodeClaim has a finalizer for cleanup purposes
    if !controllerutil.ContainsFinalizer(&nodeClaim, "karpenter.sh/finalizer") {
        controllerutil.AddFinalizer(&nodeClaim, "karpenter.sh/finalizer")
        if err := r.Update(ctx, &nodeClaim); err != nil {
            return reconcile.Result{}, err
        }
    }

    // Handle NodeClaim provisioning or deletion
    if nodeClaim.DeletionTimestamp.IsZero() {
        if nodeClaim.Status.ProviderID == "" {
            // Provision new instance using IBM Cloud Provider
            createdNodeClaim, err := r.CloudProvider.Create(ctx, &nodeClaim)
            if err != nil {
                return reconcile.Result{}, fmt.Errorf("creating node claim: %w", err)
            }
            nodeClaim.Status = createdNodeClaim.Status
            if err := r.Status().Update(ctx, &nodeClaim); err != nil {
                return reconcile.Result{}, err
            }
        }
    } else {
        // Handle deletion
        if err := r.CloudProvider.Delete(ctx, &nodeClaim); err != nil {
            return reconcile.Result{}, fmt.Errorf("deleting node claim: %w", err)
        }
        controllerutil.RemoveFinalizer(&nodeClaim, "karpenter.sh/finalizer")
        if err := r.Update(ctx, &nodeClaim); err != nil {
            return reconcile.Result{}, err
        }
    }
    return reconcile.Result{}, nil
}

func main() {
    // Get a config to talk to the apiserver
    cfg, err := config.GetConfig()
    if err != nil {
        fmt.Printf("Error getting config: %v\n", err)
        os.Exit(1)
    }

    // Create a new manager to provide shared dependencies and start components
    mgr, err := manager.New(cfg, manager.Options{})
    if err != nil {
        fmt.Printf("Error creating manager: %v\n", err)
        os.Exit(1)
    }

    // Initialize providers
    instanceTypeProvider := instancetype.NewProvider()
    instanceProvider := instance.NewProvider()
    recorder := events.NewRecorder(mgr.GetEventRecorderFor("karpenter-ibm-cloud"))

    // Create the cloud provider
    cloudProvider := ibmcloud.New(
        mgr.GetClient(),
        recorder,
        *instanceTypeProvider,
        instanceProvider,
    )

    // Create the reconciler
    reconciler := NewIBMCloudReconciler(mgr.GetClient(), cloudProvider)

    // Create a new controller
    if err := builder.ControllerManagedBy(mgr).
        For(&v1.NodeClaim{}).
        Complete(reconciler); err != nil {
        fmt.Printf("Error creating controller: %v\n", err)
        os.Exit(1)
    }

    fmt.Println("Starting manager")
    if err := mgr.Start(signals.SetupSignalHandler()); err != nil {
        fmt.Printf("Error running manager: %v\n", err)
        os.Exit(1)
    }
}
