/*
Copyright 2019 The Kubernetes Authors.

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

package machineset

import (
    "fmt"
    "context"
    "k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
    "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
    "github.com/pkg/errors"
    "sigs.k8s.io/controller-runtime/pkg/source"
	machinev1beta1 "github.com/openshift/cluster-api/pkg/apis/machine/v1beta1"
    apierrors "k8s.io/apimachinery/pkg/api/errors"
)

const (
	// controllerNameMCPS is the name of this controller
	controllerNameMCPS = "MCPS_controller"
    machineRoleLabel = "machine.openshift.io/cluster-api-machine-role"
    machineTypeLable = "machine.openshift.io/cluster-api-machine-type"
    masterMachineRoleType = "master"
)

var (
    labelsToCheck = map[string]string{
        machineRoleLabel: masterMachineRoleType,
        machineTypeLable: masterMachineRoleType,
    }
)

// ReconcileMachineSet reconciles a MachineSet object
type ReconcileMachineControlPlaneSet struct {
	client.Client
	scheme   *runtime.Scheme
	recorder record.EventRecorder
}

// newReconciler returns a new reconcile.Reconciler.
func newMCPSReconciler(mgr manager.Manager) *ReconcileMachineControlPlaneSet {
	return &ReconcileMachineControlPlaneSet{Client: mgr.GetClient(), scheme: mgr.GetScheme(), recorder: mgr.GetEventRecorderFor(controllerName)}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler.
func addMCPS(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller.
	c, err := controller.New(controllerNameMCPS, mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to MachineSet.
	return c.Watch(
		&source.Kind{Type: &machinev1beta1.MachineControlPlaneSet{}},
		&handler.EnqueueRequestForObject{},
	)
}

// Reconcile reads that state of the cluster for a MachineSet object and makes changes based on the state read
// and what is in the MachineSet.Spec
// Automatically generate RBAC rules to allow the Controller to read and write Deployments
// +kubebuilder:rbac:groups=machine.openshift.io,resources=machinesets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=machine.openshift.io,resources=machines,verbs=get;list;watch;create;update;patch;delete
func (r *ReconcileMachineControlPlaneSet) Reconcile(request reconcile.Request) (reconcile.Result, error) {
    klog.Infof("Reconciling: %v", request.NamespacedName)
    // Fetch the MachineSet instance
	ctx := context.TODO()
    if request.NamespacedName.Namespace != "openshift-machine-api" || request.NamespacedName.Name != "default" {
        klog.Error("We don't process those")
        return reconcile.Result{}, nil
    }
	mcps := &machinev1beta1.MachineControlPlaneSet{}
	if err := r.Get(ctx, request.NamespacedName, mcps); err != nil {
		if apierrors.IsNotFound(err) {
			// Object not found, return.  Created objects are automatically garbage collected.
			// For additional cleanup logic use finalizers.
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}
    klog.Infof("mcps status original: %v", mcps.Status)

    newMCPS := mcps.DeepCopy()
    allMachinesList := &machinev1beta1.MachineList{}
    if err := r.Client.List(context.Background(), allMachinesList, client.InNamespace(mcps.Namespace)); err != nil {
        return reconcile.Result{}, errors.Wrap(err, "failed to list machines")
    }
    masters := r.filterMasters(allMachinesList.Items)
    added := r.addMastersToStatus(&newMCPS.Status.ControlPlaneMachines, masters)
    if added {
        klog.Infof("updating mcps status: %v", newMCPS.Status)
        if err := r.Client.Status().Update(context.Background(), newMCPS); err != nil {
			klog.Errorf("Failed to add masters: %v", err)
			return reconcile.Result{}, err
		}
		// return since we updated, we'll reconcile again.
        return reconcile.Result{}, nil
    }

    // TODO: add finalizers to masters here.
    // We only queue requests here if a master is deleted, so don't return
    // after updating masters.

    if len(mcps.Status.ControlPlaneMachines) < 3 {
        // Less than 3 masters, don't do anything else.
        return reconcile.Result{}, nil
    }

    // We have at least 3 masters, now we need to do sanity checking and exit
    // early if things don't look right.

    inProcess := 0
    var cpInProcess *machinev1beta1.ControlPlaneMachine
    for _, cp := range newMCPS.Status.ControlPlaneMachines {
        if cp.ReplacementInProgress {
            inProcess += 1
            // Take a refernce so we can mutate it without doing crazy list splicing.
            cpInProcess = &cp
        }
    }
    if inProcess > 1 {
        klog.Errorf("Unable to proceed.  More than one master is marked as being replaced")
        return reconcile.Result{}, fmt.Errorf("Cannot process more than one replacement")
    } else if inProcess == 1 {
        return r.processReplace(newMCPS, cpInProcess)
    }

    // Nothing was found to be in process, let's look at the masters and
    // determine if we need to replace one.

    return reconcile.Result{}, nil
}

func (r *ReconcileMachineControlPlaneSet) processReplace(newMCPS *machinev1beta1.MachineControlPlaneSet, cpInProcess *machinev1beta1.ControlPlaneMachine) (reconcile.Result, error) {
    // This function does the real work.
    if len(cpInProcess.Replaces) == 0 {
        // Need to make sure this new name we're construction doesn't already exist as a machine.
        cpInProcess.Replaces = fmt.Sprintf("%sa", cpInProcess.Name)
        // Fill out the field with a new name and reconcile again next time
        klog.Infof("updating mcps status: %v", newMCPS.Status)
        if err := r.Client.Status().Update(context.Background(), newMCPS); err != nil {
			klog.Errorf("Failed to add masters: %v", err)
			return reconcile.Result{}, err
		}
    }

    newm := &machinev1beta1.Machine{}
	key := client.ObjectKey{Namespace: newMCPS.ObjectMeta.GetNamespace(), Name: cpInProcess.Name}
    err := r.Client.Get(context.Background(), key, newm)
	if err != nil {
        if apierrors.IsNotFound(err) {
			// Replacement machine not found, we need to create it.
            // Don't return here because creating a machine doesn't requeue us
            r.createReplacementMachine(newMCPS, cpInProcess)
		}
		klog.Errorf("Unable to retrieve Machine %v from store: %v", key, err)
		return reconcile.Result{}, err
	}

    return reconcile.Result{}, nil
}

func (r *ReconcileMachineControlPlaneSet) createReplacementMachine(newMCPS *machinev1beta1.MachineControlPlaneSet, cpInProcess *machinev1beta1.ControlPlaneMachine) error {

    return nil
}

func (r *ReconcileMachineControlPlaneSet) addMastersToStatus(cpSet *[]machinev1beta1.ControlPlaneMachine, masters []machinev1beta1.Machine) bool {
    added := false
    for _, machine := range masters {
        exists := false
        for _, cp := range *cpSet {
            if machine.Name == cp.Name {
                exists = true
                break
            }
        }
        if exists {
            continue
        }
        klog.Infof("adding master to status: %v", machine.Name)
        cpToAdd := machinev1beta1.ControlPlaneMachine{
            Name: machine.Name,
            ReplacementInProgress: false,
        }
        *cpSet = append(*cpSet, cpToAdd)
        added = true
    }
    return added
}

func (r *ReconcileMachineControlPlaneSet) filterMasters(machines []machinev1beta1.Machine) []machinev1beta1.Machine {
    cpMachines := []machinev1beta1.Machine{}
    for _, machine := range machines {
        if machineIsMaster(&machine) {
            cpMachines = append(cpMachines, machine)
        }
    }
    return cpMachines
}

func machineIsMaster(machine *machinev1beta1.Machine) bool {
    for key, value := range labelsToCheck {
        _, ok := machine.ObjectMeta.Labels[key]
        if !ok {
            return false
        }
        if machine.ObjectMeta.Labels[key] != value {
            return false
        }
    }
    return true
}
