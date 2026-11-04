import { apiRequest } from './apiClient';

export interface HomepageSection {
  id: string;
  sectionType: string;
  title?: string | null;
  config: Record<string, any>;
  sortOrder: number;
  isActive: boolean;
}

export const homepageApi = {
  getSections: async (): Promise<HomepageSection[]> => {
    const res = await apiRequest<{ sections: HomepageSection[] }>('/homepage-sections');
    return res.sections || [];
  },
};
