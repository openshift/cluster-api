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
	"context"
	"fmt"
	machinev1beta1 "github.com/openshift/cluster-api/pkg/apis/machine/v1beta1"
	"github.com/openshift/cluster-api/pkg/util"
	"github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const (
	// controllerNameMCPS is the name of this controller
	controllerNameMCPS    = "MCPS_controller"
	machineRoleLabel      = "machine.openshift.io/cluster-api-machine-role"
	machineTypeLable      = "machine.openshift.io/cluster-api-machine-type"
	masterMachineRoleType = "master"
	mcpsMasterFinalizer   = "machine.openshift.io/mcps-managed"
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

func (r *ReconcileMachineControlPlaneSet) addMasterFinalizers(masters []machinev1beta1.Machine) error {
	for _, m := range masters {
		if !util.Contains(m.Finalizers, mcpsMasterFinalizer) {
			m.Finalizers = append(m.ObjectMeta.Finalizers, mcpsMasterFinalizer)
			ctx := context.TODO()
			if err := r.Client.Update(ctx, &m); err != nil {
				klog.Infof("Failed to add finalizers to machine %q: %v", m.Name, err)
				return err
			}
		}
	}
	return nil
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
	// klog.Infof("mcps status original: %v", mcps.Status)

	newMCPS := mcps.DeepCopy()
	allMachinesList := &machinev1beta1.MachineList{}
	if err := r.Client.List(context.Background(), allMachinesList, client.InNamespace(mcps.Namespace)); err != nil {
		return reconcile.Result{}, errors.Wrap(err, "failed to list machines")
	}
	masters := r.filterMasters(allMachinesList.Items)

	// We only queue requests here if a master is deleted, so don't return
	// after updating master finalizers
	if err := r.addMasterFinalizers(masters); err != nil {
		return reconcile.Result{}, err
	}

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
	klog.Infof("after add Masters")
	if len(mcps.Status.ControlPlaneMachines) < 3 {
		klog.Errorf("Less than 3 masters")
		// Less than 3 masters, don't do anything else.
		return reconcile.Result{}, nil
	}

	// We have at least 3 masters, now we need to do sanity checking and exit
	// early if things don't look right.

	inProcess := 0
	var cpInProcess machinev1beta1.ControlPlaneMachine
	masterMissing := false
	var missingMasterName string
	klog.Infof("ranging cpms for validity")
	var cpIndex int
	for i, cp := range newMCPS.Status.ControlPlaneMachines {
		if cp.ReplacementInProgress {
			inProcess += 1
			// Take a refernce so we can mutate it without doing crazy list splicing.
			cpInProcess = cp
			cpIndex = i
		}
		masterFound := false
		for _, mstr := range masters {
			if mstr.Name == cp.Name {
				masterFound = true
				break
			}
		}
		if !masterFound {
			masterMissing = true
			missingMasterName = cp.Name
			break
		}
	}
	if masterMissing {
		// This means a master we knew about previously has disappeared.
		// There is no way for us to replace it, and something went seriously
		// wrong.
		klog.Errorf("Unable to proceed.  A previously discovered master is missing: %s", missingMasterName)
		// Should set some status and emit an event that this is fatal.
		return reconcile.Result{}, nil
	}
	if inProcess > 1 {
		klog.Errorf("Unable to proceed.  More than one master is marked as being replaced")
		return reconcile.Result{}, fmt.Errorf("Cannot process more than one replacement")
	} else if inProcess == 1 {
		klog.Infof("We're replacing something")
		return r.processReplace(newMCPS, cpInProcess, cpIndex)
	}

	// Nothing was found to be in process, let's look at the masters and
	// determine if we need to replace one.
	var masterToReplace string
	for _, mstr := range masters {
		// More than one master can have a deletion timestamp, that's okay.
		// We'll just process them one at a time.
		if !mstr.ObjectMeta.DeletionTimestamp.IsZero() {
			klog.Infof("Found master with deletion timestamp")
			masterToReplace = mstr.Name
			break
		}
	}
	if masterToReplace != "" {
		klog.Infof("about to update status with master")
		// We found a master with deletion timestamp, let's update our status.
		for i, cp := range newMCPS.Status.ControlPlaneMachines {
			if cp.Name == masterToReplace {
				cp.ReplacementInProgress = true
				// We're going to update our status so we requeue to process replacement.
				newMCPS.Status.ControlPlaneMachines[i] = cp
				if err := r.Client.Status().Update(context.Background(), newMCPS); err != nil {
					klog.Errorf("Failed to add masters: %v", err)
					return reconcile.Result{}, err
				}
				// return since we updated, we'll reconcile again.
				return reconcile.Result{}, nil
			}

		}
	}

	// No masters need to be replaced, return nil.
	return reconcile.Result{}, nil
}

func (r *ReconcileMachineControlPlaneSet) processReplace(newMCPS *machinev1beta1.MachineControlPlaneSet, cpInProcess machinev1beta1.ControlPlaneMachine, cpIndex int) (reconcile.Result, error) {
	// This function does the real work.
	klog.Infof("processing replace of %v", cpInProcess)
	if len(cpInProcess.Replaces) == 0 {
		// Need to make sure this new name we're construction doesn't already exist as a machine.
		// This part is tricky....
		// Might want to create new machine, give it an annotation of some kind,
		// then scrape the name.
		cpInProcess.Replaces = fmt.Sprintf("%sa", cpInProcess.Name)
		newMCPS.Status.ControlPlaneMachines[cpIndex] = cpInProcess
		// Fill out the field with a new name and reconcile again next time
		klog.Infof("updating mcps status: %v", newMCPS.Status)
		if err := r.Client.Status().Update(context.Background(), newMCPS); err != nil {
			klog.Errorf("Failed to add masters: %v", err)
			return reconcile.Result{}, err
		}
	}

	newm := &machinev1beta1.Machine{}
	key := client.ObjectKey{Namespace: newMCPS.ObjectMeta.GetNamespace(), Name: cpInProcess.Replaces}
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

func (r *ReconcileMachineControlPlaneSet) createReplacementMachine(newMCPS *machinev1beta1.MachineControlPlaneSet, cpInProcess machinev1beta1.ControlPlaneMachine) error {
	klog.Infof("Replacement Creation Called")
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
			Name:                  machine.Name,
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
