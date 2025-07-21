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
	"strings"
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

func addAsAppCertificate(client Client, cfg *config.Config) error {
	var certName = cfg.CertName()
	ID, ok := certsList[certName]
	if !ok {
		return fmt.Errorf("certificate %s not found in the certificates list", certName)
	}
	args := []interface{}{}
	var response AppListQueryResponse
	resp, err := client.Call("app.query", cfg.TimeoutSeconds, args)
	if err != nil {
		return fmt.Errorf("app query failed, %v", err)
	}
	if cfg.Debug {
		log.Printf("app query response: %v", string(resp))
	}
	err = json.Unmarshal(resp, &response)
	if err != nil {
		return fmt.Errorf("app query failed, %v, ", err)
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
			
			if currentCertID == ID {
				if cfg.Debug {
					log.Printf("App %s already has the correct certificate (ID: %d), skipping update", app["name"], ID)
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
			currentConfig["certificate_id"] = ID
			
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

			// Monitor the progress of the job.
			jobCompleted := false
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
				}
			}

			log.Printf("updated the certificate for app: %s to use: %s, id: %v", app["name"], certName,
				certsList[certName])
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

func addAsUICertificate(client Client, cfg *config.Config) (bool, error) {
	var certName = cfg.CertName()
	ID, ok := certsList[certName]
	if !ok {
		return false, fmt.Errorf("certificate %s not found in the certificates list", certName)
	}
	pmap := map[string]int64{
		"ui_certificate": ID,
	}
	args := []interface{}{pmap}
	_, err := client.Call("system.general.update", cfg.TimeoutSeconds, args)
	if err != nil {
		return false, fmt.Errorf("system.general.update of ui_certificate failed, %v", err)
	}
	return true, nil
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

	// Monitor the progress of the job.
	jobCompleted := false
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

		// Monitor the progress of the job.
		jobCompleted := false
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
			}
		}
	}
	return nil
}

// poll and save all deployed certificates matching our Cert_basename
// including the newly created certificate
func loadCertificateList(client Client, cfg *config.Config) error {
	var inlist = false
	var certName = cfg.CertName()
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
		if id, ok := certsList[certName]; ok == true {
			log.Printf("found new certificate, %v, id: %d", cert["name"], id)
			inlist = true
		}
	}

	if !inlist {
		return fmt.Errorf("certificate search failed, certificate %s was not deployed", certName)
	} else {
		log.Printf("certificate %s deployed successfully", certName)
	}
	return nil
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

	// First check if a certificate with this name already exists and is current
	err = loadCertificateList(client, cfg)
	if err == nil {
		// Certificate already exists, skip creation
		if _, exists := certsList[certName]; exists {
			log.Printf("certificate %s already exists, skipping creation", certName)
		} else {
			// deploy the certificate only if it doesn't exist
			err = createCertificate(client, cfg)
			if err != nil {
				return err
			}
			// reload the certificate list after creation
			err = loadCertificateList(client, cfg)
			if err != nil {
				return err
			}
		}
	} else {
		// deploy the certificate
		err = createCertificate(client, cfg)
		if err != nil {
			return err
		}
		// load the certificate list
		err = loadCertificateList(client, cfg)
		if err != nil {
			return err
		}
	}

	if cfg.AddAsUiCertificate {
		activated, err = addAsUICertificate(client, cfg)
		if err != nil {
			return err
		}
	}

	if cfg.AddAsFTPCertificate {
		err := addAsFTPCertificate(client, cfg)
		if err != nil {
			return err
		}
	}

	if cfg.AddAsAppCertificate {
		err := addAsAppCertificate(client, cfg)
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
