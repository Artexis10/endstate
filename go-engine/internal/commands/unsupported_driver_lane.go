// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"fmt"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/driver"
	"github.com/Artexis10/endstate/go-engine/internal/events"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/planner"
)

func firstAppSlice(apps [][]manifest.App) []manifest.App {
	if len(apps) == 0 {
		return nil
	}
	return apps[0]
}

func unsupportedDriverDetails(app manifest.App) (driverName, ref, name, message string) {
	driverName = strings.ToLower(strings.TrimSpace(app.Driver))
	switch driverName {
	case "winget", "chocolatey":
		ref = app.Refs["windows"]
	case "brew":
		ref = app.Refs["darwin"]
	default:
		ref = resolveAppRef(app)
	}
	name = resolveItemDisplayName("", app, ref)
	message = fmt.Sprintf("%s driver unavailable on this host", driverName)
	return
}

func planUnsupportedDriverApply(apps []manifest.App, emitter *events.Emitter) []ApplyAction {
	actions := make([]ApplyAction, 0, len(apps))
	for _, app := range apps {
		driverName, ref, name, message := unsupportedDriverDetails(app)
		actions = append(actions, ApplyAction{
			ID: app.ID, Ref: refPtrOrNil(ref), Driver: driverName, Name: name,
			Status: driver.StatusSkipped, Reason: driver.ReasonFiltered, Message: message,
		})
		emitter.EmitItem(brewEventID(app.ID, ref), driverName, driver.StatusSkipped, driver.ReasonFiltered, message, name)
	}
	return actions
}

func planUnsupportedDrivers(apps []manifest.App, emitter *events.Emitter) []planner.PlanAction {
	actions := make([]planner.PlanAction, 0, len(apps))
	for _, app := range apps {
		driverName, ref, name, message := unsupportedDriverDetails(app)
		actions = append(actions, planner.PlanAction{
			Type: "app", ID: app.ID, Ref: ref, Driver: driverName,
			CurrentStatus: driver.StatusSkipped, PlannedAction: "skip", DisplayName: name, Message: message,
		})
		emitter.EmitItem(brewEventID(app.ID, ref), driverName, driver.StatusSkipped, driver.ReasonFiltered, message, name)
	}
	return actions
}

func verifyUnsupportedDrivers(apps []manifest.App, emitter *events.Emitter) []VerifyItem {
	items := make([]VerifyItem, 0, len(apps))
	for _, app := range apps {
		driverName, ref, name, message := unsupportedDriverDetails(app)
		items = append(items, VerifyItem{
			Type: "app", ID: app.ID, Ref: ref, Driver: driverName, Name: name,
			Status: driver.StatusSkipped, Reason: driver.ReasonFiltered, Message: message,
		})
		emitter.EmitItem(brewEventID(app.ID, ref), driverName, driver.StatusSkipped, driver.ReasonFiltered, message, name)
	}
	return items
}
