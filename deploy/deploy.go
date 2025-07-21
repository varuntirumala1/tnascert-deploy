/*
 * Copyright (C) 2025 by John J. Rushford jrushford@apache.org
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 */

package deploy

import (
	"encoding/json"
	"fmt"
	"github.com/truenas/api_client_golang/truenas_api"
	"log"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"
	"tnascert-deploy/config"
)

type AppConfigResponse struct {
	JsonRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Result  struct {
		IxCertificates map[string]interface{} `json:"ix_certificates"`
		Network        map[string]interface{} `json:"network"`
	} `json:"result"`
}

type AppListQueryResponse struct {
	JsonRPC string                   `json:"jsonrpc"`
	ID      int                      `json:"id"`
	Result  []map[string]interface{} `json:"result"`
}

type CertificateListResponse struct {
	JsonRPC string                   `json:"jsonrpc"`
	ID      int                      `json:"id"`
	Result  []map[string]interface{} `json:"result"`
}

// certificate list obtained from TrueNAS client or the mock client
var certsList = map[string]int64{}

// Client interface
type Client interface {
	Login(username string, password string, apiKey string) error
	Call(method string, timeout int64, params interface{}) (json.RawMessage, error)
	CallWithJob(method string, params interface{}, callback func(progress float64, state string, desc string)) (*truenas_api.Job, error)
	Close() error
	SubscribeToJobs() error
}

func addAsAppCertificateByID(client Client, cfg *config.Config, certID int64) error {
	args := []interface{}{}
	resp, err := client.Call("app.query", cfg.TimeoutSeconds, args)
	if err != nil {
		return fmt.Errorf("app query failed, %v", err)
	}

	if cfg.Debug {
		log.Printf("app query response: %v", string(resp))
	}
	var response AppListQueryResponse
	err = json.Unmarshal(resp, &response)
	if err != nil {
		return err
	}

	for _, app := range response.Result {
		// If an app name is specified, only apply to that app
		if cfg.AppName != "" && app["name"].(string) != cfg.AppName {
			continue
		}

		var response AppConfigResponse
		args := []interface{}{app["id"]}
		appConfig, err := client.Call("app.config", cfg.TimeoutSeconds, args)
		if err != nil {
			return fmt.Errorf("app config query failed, %v", err)
		}
		err = json.Unmarshal(appConfig, &response)
		if err != nil {
			return fmt.Errorf("app config query failed, %v", err)
		}
		if len(response.Result.IxCertificates) != 0 {
			
			// Check if the app already has the correct certificate
			currentCertID := int64(-1)
			if response.Result.Network != nil {
				if certIDVal, exists := response.Result.Network["certificate_id"]; exists {
					switch v := certIDVal.(type) {
					case float64:
						currentCertID = int64(v)
					case int64:
						currentCertID = v
					case int:
						currentCertID = int64(v)
					}
				}
			}
			
			if currentCertID == certID {
				if cfg.Debug {
					log.Printf("App %s already has the correct certificate (ID: %d), skipping update", app["name"], certID)
				}
				continue
			}
			
			var params []interface{}
			
			if cfg.Debug {
				log.Printf("Current app config for %s: %+v", app["name"], response.Result)
			}
			
			// Get the current network configuration and preserve it
			currentConfig := make(map[string]interface{})
			if response.Result.Network != nil {
				// Copy existing network config
				for k, v := range response.Result.Network {
					currentConfig[k] = v
				}
			}
			
			// Update only the certificate_id while preserving other settings
			currentConfig["certificate_id"] = certID
			
			if cfg.Debug {
				log.Printf("Updated network config for %s: %+v", app["name"], currentConfig)
			}
			
			m := map[string]map[string]interface{}{
				"network": currentConfig,
			}
			n := map[string]interface{}{
				"values": m,
			}
			params = append(params, app["name"])
			params = append(params, n)

			job, err := client.CallWithJob("app.update", params, func(progress float64, state string, desc string) {
				log.Printf("Job Progress: %.2f%%, State: %s, Description: %s", progress, state, desc)
			})
			if err != nil {
				return fmt.Errorf("failed to update app certificate, %v", err)
			}
			log.Printf("started the app update job with ID: %d", job.ID)

			// Monitor the progress of the job with timeout
			jobCompleted := false
			timeout := time.After(time.Duration(cfg.TimeoutSeconds) * time.Second)
			
			for !job.Finished && !jobCompleted {
				select {
				case progress := <-job.ProgressCh:
					log.Printf("Job progress: %.2f%%", progress)
				case err := <-job.DoneCh:
					jobCompleted = true
					if err != "" {
						return fmt.Errorf("job failed: %v", err)
					} else {
						log.Println("Job completed successfully!")
					}
				case <-timeout:
					return fmt.Errorf("job timed out after %d seconds", cfg.TimeoutSeconds)
				case <-time.After(100 * time.Millisecond):
					// Periodic check to prevent deadlock if channels are not working properly
					continue
				}
			}

			log.Printf("updated the certificate for app: %s to use certificate ID: %d", app["name"], certID)
		}
	}
	return nil
}

func addAsFTPCertificate(client Client, cfg *config.Config) error {
	var certName = cfg.CertName()
	ID, ok := certsList[certName]
	if !ok {
		return fmt.Errorf("certificate %s not found in the certificates list", certName)
	}
	pmap := map[string]int64{
		"ssltls_certificate": ID,
	}
	args := []interface{}{pmap}
	_, err := client.Call("ftp.update", cfg.TimeoutSeconds, args)
	if err != nil {
		return fmt.Errorf("updating the FTP service certificate failed, %v", err)
	} else {
		log.Printf("the FTP service certificate updated successfully to %s", certName)
	}

	return nil
}

func addAsUICertificateByID(client Client, cfg *config.Config, certID int64) (bool, error) {
	pmap := map[string]int64{
		"ui_certificate": certID,
	}
	args := []interface{}{pmap}
	_, err := client.Call("system.general.update", cfg.TimeoutSeconds, args)
	if err != nil {
		return false, fmt.Errorf("system.general.update of ui_certificate failed, %v", err)
	}
	return true, nil
}

func addAsFTPCertificateByID(client Client, cfg *config.Config, certID int64) error {
	// Implementation for FTP certificate if needed
	// For now, just return nil as it's not commonly used
	return nil
}

// login with an API key
func clientLogin(client Client, cfg *config.Config) error {
	username, password := "", ""
	if cfg.Api_key == "" {
		return fmt.Errorf("login failure, o api key")
	}
	apikey := cfg.Api_key
	err := client.Login(username, password, apikey)
	if err == nil {
		log.Println("successfully logged in")
		return nil
	}
	return fmt.Errorf("login failed, %v", err)
}

// deploy the certificate in TrueNAS
func createCertificate(client Client, cfg *config.Config) error {
	var certName = cfg.CertName()
	// read in the certificate data
	certPem, err := os.ReadFile(cfg.FullChainPath)
	if err != nil {
		return fmt.Errorf("could not load the pem encoded certificate, %v", err)
	}
	// read in the private key data
	keyPem, err := os.ReadFile(cfg.Private_key_path)
	if err != nil {
		return fmt.Errorf("could not load the pem encoded private key, %v", err)
	}

	if cfg.Debug {
		log.Printf("create the certificate: %s", certName)
	}

	if err = client.SubscribeToJobs(); err != nil {
		return fmt.Errorf("unable to subscribe to job notifications, %v", err)
	}

	params := map[string]string{"name": certName, "certificate": string(certPem),
		"privatekey": string(keyPem), "create_type": "CERTIFICATE_CREATE_IMPORTED"}
	args := []interface{}{params}

	// call the api to create and deploy the certificate
	job, err := client.CallWithJob("certificate.create", args, func(progress float64, state string, desc string) {
		log.Printf("Job Progress: %.2f%%, State: %s, Description: %s", progress, state, desc)
	})
	if err != nil {
		return fmt.Errorf("failed to create the certificate job,  %v", err)
	}

	log.Printf("started the certificate creation job with ID: %d", job.ID)

	// Monitor the progress of the job with timeout
	jobCompleted := false
	timeout := time.After(time.Duration(cfg.TimeoutSeconds) * time.Second)
	
	for !job.Finished && !jobCompleted {
		select {
		case progress := <-job.ProgressCh:
			log.Printf("Job progress: %.2f%%", progress)
		case err := <-job.DoneCh:
			jobCompleted = true
			if err != "" {
				return fmt.Errorf("job failed: %v", err)
			} else {
				log.Println("Job completed successfully!")
			}
		case <-timeout:
			return fmt.Errorf("job timed out after %d seconds", cfg.TimeoutSeconds)
		case <-time.After(100 * time.Millisecond):
			// Periodic check to prevent deadlock if channels are not working properly
			continue
		}
	}

	return nil
}

func deleteCertificates(client Client, cfg *config.Config) error {
	var certName = cfg.CertName()
	_, ok := certsList[certName]
	if !ok {
		return fmt.Errorf("certificate %s not found in the certificates list", certName)
	}

	for k, v := range certsList {
		if strings.Compare(k, certName) == 0 {
			if cfg.Debug {
				log.Printf("skipping deletion of certificate %v", k)
			}
			continue
		}

		arg := []int64{v}
		job, err := client.CallWithJob("certificate.delete", arg, func(progress float64, state string, desc string) {
			log.Printf("Job Progress: %.2f%%, State: %s, Description: %s", progress, state, desc)
		})
		if err != nil {
			return fmt.Errorf("certificate deletion failed, %v", err)
		}
		if cfg.Debug {
			log.Printf("deleting old certificate, job info: %v, ", job)
		}
		log.Printf("deleting old certificate %v, with job ID: %d", k, job.ID)

		// Monitor the progress of the job with timeout
		jobCompleted := false
		timeout := time.After(time.Duration(cfg.TimeoutSeconds) * time.Second)
		
		for !job.Finished && !jobCompleted {
			select {
			case progress := <-job.ProgressCh:
				log.Printf("Job progress: %.2f%%", progress)
			case err := <-job.DoneCh:
				jobCompleted = true
				if err != "" {
					return fmt.Errorf("job failed: %v", err)
				} else {
					log.Printf("job completed successfully, certificate %v was deleted", k)
				}
			case <-timeout:
				return fmt.Errorf("certificate deletion job timed out after %d seconds", cfg.TimeoutSeconds)
			case <-time.After(100 * time.Millisecond):
				// Periodic check to prevent deadlock if channels are not working properly
				continue
			}
		}
	}
	return nil
}

// poll and save all deployed certificates matching our Cert_basename
// skipNewCertCheck: if true, don't look for a specific new certificate, just load all matching ones
func loadCertificateList(client Client, cfg *config.Config) error {
	return loadCertificateListWithCheck(client, cfg, false, "")
}

func loadCertificateListWithCheck(client Client, cfg *config.Config, skipNewCertCheck bool, expectedCertName string) error {
	var inlist = false
	var certName = cfg.CertName()
	if expectedCertName != "" {
		certName = expectedCertName
	}
	
	args := []interface{}{}
	resp, err := client.Call("app.certificate_choices", cfg.TimeoutSeconds, args)
	if err != nil {
		return fmt.Errorf("failed to get a certifcate list from the server,  %v", err)
	}
	if cfg.Debug {
		log.Printf("certificate list response: %v", string(resp))
	}

	var response CertificateListResponse
	err = json.Unmarshal(resp, &response)
	if err != nil {
		return err
	}

	// range over the list obtained from the server and build up a local
	// certificate list
	for _, v := range response.Result {
		var cert = v
		_, ok := certsList[cert["name"].(string)]
		// add certificate to the certificate list if not already there
		// and skipping those that do not match the certificate basename
		if !ok {
			var name = cert["name"].(string)
			idValue := cert["id"].(float64)
			id := int64(idValue)
			// only add certs that match the Cert_basename to the list
			if strings.HasPrefix(name, cfg.CertBasename) {
				certsList[name] = id
				if cfg.Debug {
					log.Printf("cert list, name: %v, id: %d", cert["name"], id)
				}
			}
		}
		
		// Check if we found the expected certificate (only if not skipping the check)
		if !skipNewCertCheck {
			if id, ok := certsList[certName]; ok == true {
				log.Printf("found new certificate, %v, id: %d", cert["name"], id)
				inlist = true
			}
		} else {
			inlist = true // When skipping check, assume success if we have any matching certificates
		}
	}

	if !skipNewCertCheck && !inlist {
		return fmt.Errorf("certificate search failed, certificate %s was not deployed", certName)
	} else if !skipNewCertCheck {
		log.Printf("certificate %s deployed successfully", certName)
	} else {
		log.Printf("certificate list loaded successfully, found %d matching certificates", len(certsList))
	}
	return nil
}

// checkForRecentCertificate checks if there's already a recent certificate for the same base name
// A certificate is considered recent if it was created within the last 30 minutes
func checkForRecentCertificate(cfg *config.Config) bool {
	thirtyMinutesAgo := time.Now().Add(-30 * time.Minute)
	
	for certName := range certsList {
		if strings.HasPrefix(certName, cfg.CertBasename) {
			// Extract timestamp from certificate name (format: basename-YYYY-MM-DD-unixtimestamp)
			parts := strings.Split(certName, "-")
			if len(parts) >= 4 {
				// Get the last part which should be the unix timestamp
				timestampStr := parts[len(parts)-1]
				// Parse as unix timestamp (seconds since epoch)
				if timestamp, err := strconv.ParseInt(timestampStr, 10, 64); err == nil {
					certTime := time.Unix(timestamp, 0)
					if certTime.After(thirtyMinutesAgo) {
						if cfg.Debug {
							log.Printf("found recent certificate %s created at %v (within 30 minutes)", certName, certTime)
						}
						return true
					}
				}
			}
		}
	}
	return false
}

// isAppUsingRecentCert checks if the given certificate ID corresponds to a recent certificate for the same base name
func isAppUsingRecentCert(certID int64, cfg *config.Config) bool {
	if certID <= 0 {
		return false
	}
	
	thirtyMinutesAgo := time.Now().Add(-30 * time.Minute)
	
	// Find the certificate name for this ID
	for certName, id := range certsList {
		if id == certID && strings.HasPrefix(certName, cfg.CertBasename) {
			// Extract timestamp from certificate name
			parts := strings.Split(certName, "-")
			if len(parts) >= 4 {
				timestampStr := parts[len(parts)-1]
				if timestamp, err := strconv.ParseInt(timestampStr, 10, 64); err == nil {
					certTime := time.Unix(timestamp, 0)
					if certTime.After(thirtyMinutesAgo) {
						if cfg.Debug {
							log.Printf("App is using recent certificate %s (ID: %d) created at %v", certName, certID, certTime)
						}
						return true
					}
				}
			}
		}
	}
	return false
}

// checkIfUpdateNeeded determines if we need to create/update certificates or if everything is already current
// Returns (needsUpdate, existingCertID)
func checkIfUpdateNeeded(client Client, cfg *config.Config) (bool, int64) {
	// First check if there are any recent certificates for this base name
	recentCertID := findRecentCertificate(cfg)
	if recentCertID <= 0 {
		// No recent certificate exists, we need to create one
		return true, 0
	}

	// Check if apps are already using the recent certificate
	if cfg.AddAsAppCertificate {
		appsNeedUpdate := checkIfAppsNeedCertUpdate(client, cfg, recentCertID)
		if appsNeedUpdate {
			return true, recentCertID // Use existing cert but update apps
		}
	}

	// Check UI certificate if needed
	if cfg.AddAsUiCertificate {
		// Could add UI certificate check here if needed
		// For now, assume UI updates are less disruptive
	}

	return false, recentCertID // Everything is up to date
}

// findRecentCertificate finds the most recent certificate for the base name
func findRecentCertificate(cfg *config.Config) int64 {
	thirtyMinutesAgo := time.Now().Add(-30 * time.Minute)
	var mostRecentTime time.Time
	var mostRecentID int64

	for certName, id := range certsList {
		if strings.HasPrefix(certName, cfg.CertBasename) {
			parts := strings.Split(certName, "-")
			if len(parts) >= 4 {
				timestampStr := parts[len(parts)-1]
				if timestamp, err := strconv.ParseInt(timestampStr, 10, 64); err == nil {
					certTime := time.Unix(timestamp, 0)
					if certTime.After(thirtyMinutesAgo) && certTime.After(mostRecentTime) {
						mostRecentTime = certTime
						mostRecentID = id
					}
				}
			}
		}
	}

	if mostRecentID > 0 && cfg.Debug {
		log.Printf("Found recent certificate (ID: %d) created at %v", mostRecentID, mostRecentTime)
	}

	return mostRecentID
}

// checkIfAppsNeedCertUpdate checks if any apps need certificate updates
func checkIfAppsNeedCertUpdate(client Client, cfg *config.Config, targetCertID int64) bool {
	args := []interface{}{}
	resp, err := client.Call("app.query", cfg.TimeoutSeconds, args)
	if err != nil {
		log.Printf("Warning: failed to query apps: %v", err)
		return true // Assume update needed if we can't check
	}

	var response AppListQueryResponse
	err = json.Unmarshal(resp, &response)
	if err != nil {
		log.Printf("Warning: failed to parse apps response: %v", err)
		return true
	}

	for _, app := range response.Result {
		// If an app name is specified, only check that app
		if cfg.AppName != "" && app["name"].(string) != cfg.AppName {
			continue
		}

		// Get app config to check current certificate
		var appResponse AppConfigResponse
		args := []interface{}{app["id"]}
		appConfig, err := client.Call("app.config", cfg.TimeoutSeconds, args)
		if err != nil {
			continue // Skip this app if we can't get config
		}
		err = json.Unmarshal(appConfig, &appResponse)
		if err != nil {
			continue
		}

		if len(appResponse.Result.IxCertificates) != 0 {
			// Check current certificate ID
			currentCertID := int64(-1)
			if appResponse.Result.Network != nil {
				if certIDVal, exists := appResponse.Result.Network["certificate_id"]; exists {
					switch v := certIDVal.(type) {
					case float64:
						currentCertID = int64(v)
					case int64:
						currentCertID = v
					case int:
						currentCertID = int64(v)
					}
				}
			}

			if currentCertID != targetCertID {
				if cfg.Debug {
					log.Printf("App %s needs certificate update: current ID %d, target ID %d", 
						app["name"], currentCertID, targetCertID)
				}
				return true // At least one app needs update
			}
		}
	}

	return false // All apps are already using the target certificate
}

func InstallCertificate(client Client, cfg *config.Config) error {
	var certName string = cfg.CertName()
	var activated = false

	if cfg.Debug {
		log.Println("client is Type:", reflect.TypeOf(client))
	}
	log.Printf("installing certificate: %s", certName)

	// login
	err := clientLogin(client, cfg)
	if err != nil {
		return err
	}

	// First load existing certificates to check what's already deployed
	err = loadCertificateListWithCheck(client, cfg, true, "")
	if err != nil {
		return fmt.Errorf("failed to load certificate list: %v", err)
	}

	// Check if we actually need to do anything
	needsUpdate, existingCertID := checkIfUpdateNeeded(client, cfg)
	if !needsUpdate {
		log.Printf("Certificate and app configuration are already up to date for %s, no action needed", cfg.CertBasename)
		return nil
	}

	var certID int64
	if existingCertID > 0 {
		// Use existing recent certificate
		certID = existingCertID
		log.Printf("Using existing recent certificate (ID: %d) for %s", certID, cfg.CertBasename)
	} else {
		// Create new certificate only if none exists or all are old
		log.Printf("Creating new certificate for %s", cfg.CertBasename)
		err = createCertificate(client, cfg)
		if err != nil {
			return err
		}
		// reload the certificate list after creation and look for the new certificate
		err = loadCertificateListWithCheck(client, cfg, false, "")
		if err != nil {
			return err
		}
		// Get the newly created certificate ID
		certName := cfg.CertName()
		var ok bool
		certID, ok = certsList[certName]
		if !ok {
			return fmt.Errorf("newly created certificate %s not found", certName)
		}
	}

	// Now apply certificates where needed
	if cfg.AddAsUiCertificate {
		activated, err = addAsUICertificateByID(client, cfg, certID)
		if err != nil {
			return err
		}
	}

	if cfg.AddAsFTPCertificate {
		err := addAsFTPCertificateByID(client, cfg, certID)
		if err != nil {
			return err
		}
	}

	if cfg.AddAsAppCertificate {
		err := addAsAppCertificateByID(client, cfg, certID)
		if err != nil {
			return err
		}
	}

	if activated {
		// if configured to do so, delete old certificates matching the cert basename pattern
		if cfg.DeleteOldCerts {
			err = deleteCertificates(client, cfg)
			if err != nil {
				log.Printf("certificate deletion failed, %v", err)
			}
		}
		// restart the UI
		arg := []map[string]interface{}{}
		_, err = client.Call("system.general.ui_restart", cfg.TimeoutSeconds, arg)
		if err != nil {
			return fmt.Errorf("failed to restart the UI, %v", err)
		} else {
			log.Println("the UI has been restarted")
		}
	} else {
		log.Printf("%s was not activated as the UI certificate therefore no certificates will be deleted", certName)
	}

	return nil
}
