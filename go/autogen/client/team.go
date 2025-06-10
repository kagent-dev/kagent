package client

import (
	"fmt"
)

func (c *client) ListTeams(userID string) ([]*Team, error) {
	var teams []*Team
	err := c.doRequest("GET", fmt.Sprintf("/teams/?user_id=%s", userID), nil, &teams)
	return teams, err
}

func (c *client) CreateTeam(team *Team) error {
	return c.doRequest("POST", "/teams/", team, team)
}

func (c *client) GetTeamByID(teamID int, userID string) (*Team, error) {
	var team *Team
	err := c.doRequest("GET", fmt.Sprintf("/teams/%d?user_id=%s", teamID, userID), nil, &team)
	return team, err
}

func (c *client) GetTeam(teamLabel string, userID string) (*Team, error) {
	allTeams, err := c.ListTeams(userID)
	if err != nil {
		return nil, err
	}

	for _, team := range allTeams {
		if team.Component.Label == teamLabel {
			return team, nil
		}
	}

	return nil, nil
}

func (c *client) DeleteTeam(teamID int, userID string) error {
	return c.doRequest("DELETE", fmt.Sprintf("/teams/%d?user_id=%s", teamID, userID), nil, nil)
}
